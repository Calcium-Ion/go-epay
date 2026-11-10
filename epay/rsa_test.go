package epay

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试密钥在运行时生成，仓库中不保存任何密钥。
var (
	testKeysOnce    sync.Once
	testMerchantKey *rsa.PrivateKey
	testPlatformKey *rsa.PrivateKey
)

func testKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	testKeysOnce.Do(func() {
		var err error
		testMerchantKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testPlatformKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
	})
	return testMerchantKey, testPlatformKey
}

// bareBase64 返回去掉 PEM 首尾行的单行 Base64 密钥，即商户后台常见的复制格式。
func bareBase64(der []byte) string {
	return base64.StdEncoding.EncodeToString(der)
}

func pemText(blockType string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
}

func pkcs8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return der
}

func pkix(t *testing.T, key *rsa.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	require.NoError(t, err)
	return der
}

func newRSAClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	merchantKey, platformKey := testKeys(t)
	client, err := NewClient(&Config{
		PartnerID:          "1001",
		SignType:           SignTypeRSA,
		MerchantPrivateKey: bareBase64(pkcs8(t, merchantKey)),
		PlatformPublicKey:  bareBase64(pkix(t, &platformKey.PublicKey)),
	}, baseURL)
	require.NoError(t, err)
	return client
}

func sha256WithRSA(t *testing.T, key *rsa.PrivateKey, content string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(signature)
}

func TestRSASignParams(t *testing.T) {
	merchantKey, _ := testKeys(t)
	client := newRSAClient(t, "https://pay.example.com")
	input := map[string]string{
		"out_trade_no": "order-1",
		"money":        "1.00",
		"name":         "商品 + 50% = x",
		"empty":        "",
		"blank":        " \t",
		"sign":         "stale",
		"sign_type":    "MD5",
	}
	before := copyParams(input)

	signed, err := client.SignParams(input)
	require.NoError(t, err)
	assert.Equal(t, before, input, "SignParams must not mutate its input")
	assert.Equal(t, "1001", signed["pid"])
	assert.Equal(t, SignTypeRSA, signed["sign_type"])
	timestamp, err := strconv.ParseInt(signed["timestamp"], 10, 64)
	require.NoError(t, err)
	assert.Len(t, signed["timestamp"], 10)
	assert.InDelta(t, time.Now().Unix(), timestamp, 5)

	// sign、sign_type 和空白值不参与签名，其余参数按参数名升序拼接原始值。
	content := "money=1.00&name=商品 + 50% = x&out_trade_no=order-1&pid=1001&timestamp=" + signed["timestamp"]
	// PKCS#1 v1.5 签名是确定性的，同一私钥对同一字符串的签名唯一。
	assert.Equal(t, sha256WithRSA(t, merchantKey, content), signed["sign"])

	_, err = client.SignParams(map[string]string{"return_url": "https://merchant.example.com/?a=1&b=2"})
	assert.ErrorIs(t, err, ErrInvalidParams)
	_, err = client.SignParams(nil)
	assert.ErrorIs(t, err, ErrInvalidParams)
}

func TestRSAPurchase(t *testing.T) {
	merchantKey, _ := testKeys(t)
	client := newRSAClient(t, "https://pay.example.com/gateway/")

	paymentURL, params, err := client.Purchase(validPurchaseArgs())
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/gateway/api/pay/submit", paymentURL)
	assert.Equal(t, SignTypeRSA, params["sign_type"])
	content := "device=pc&money=1.00&name=商品名称&notify_url=https://merchant.example.com/notify&out_trade_no=order-1&pid=1001" +
		"&return_url=https://merchant.example.com/return&timestamp=" + params["timestamp"] + "&type=wxpay"
	assert.Equal(t, sha256WithRSA(t, merchantKey, content), params["sign"])

	args := validPurchaseArgs()
	args.ReturnUrl.RawQuery = "next=/&trade_status=TRADE_SUCCESS"
	paymentURL, params, err = client.Purchase(args)
	assert.ErrorIs(t, err, ErrInvalidParams)
	assert.Empty(t, paymentURL)
	assert.Nil(t, params)
}

func signedCallback(t *testing.T, key *rsa.PrivateKey, timestamp int64) map[string]string {
	t.Helper()
	ts := strconv.FormatInt(timestamp, 10)
	callback := map[string]string{
		"pid":          "1001",
		"trade_no":     "2024072320222180092",
		"out_trade_no": "order-1",
		"api_trade_no": "4200002345202407238253501450",
		"type":         "alipay",
		"trade_status": StatusTradeSuccess,
		"addtime":      "2024-07-01 16:47:32",
		"name":         "商品 + 50%",
		"money":        "1.00",
		"param":        "",
		"timestamp":    ts,
		"sign_type":    SignTypeRSA,
	}
	content := "addtime=2024-07-01 16:47:32&api_trade_no=4200002345202407238253501450&money=1.00&name=商品 + 50%" +
		"&out_trade_no=order-1&pid=1001&timestamp=" + ts + "&trade_no=2024072320222180092&trade_status=TRADE_SUCCESS&type=alipay"
	callback["sign"] = sha256WithRSA(t, key, content)
	return callback
}

func TestRSAVerifyCallback(t *testing.T) {
	merchantKey, platformKey := testKeys(t)
	client := newRSAClient(t, "https://pay.example.com")
	now := time.Now().Unix()
	merchantSign := signedCallback(t, merchantKey, now)["sign"]

	callback := signedCallback(t, platformKey, now)
	before := copyParams(callback)
	result, err := client.Verify(callback)
	require.NoError(t, err)
	assert.True(t, result.VerifyStatus)
	assert.Equal(t, "order-1", result.ServiceTradeNo)
	assert.Equal(t, "2024072320222180092", result.TradeNo)
	assert.Equal(t, StatusTradeSuccess, result.TradeStatus)
	assert.Equal(t, "1.00", result.Money)
	assert.Equal(t, before, callback, "Verify must not mutate its input")

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"tampered money", func(p map[string]string) { p["money"] = "100.00" }},
		{"tampered order", func(p map[string]string) { p["out_trade_no"] = "order-2" }},
		{"tampered timestamp", func(p map[string]string) { p["timestamp"] = strconv.FormatInt(now+1, 10) }},
		{"unsigned extra field", func(p map[string]string) { p["buyer"] = "openid" }},
		{"dropped field", func(p map[string]string) { delete(p, "api_trade_no") }},
		{"missing sign", func(p map[string]string) { delete(p, "sign") }},
		{"sign not base64", func(p map[string]string) { p["sign"] = "not base64!" }},
		{"signed by merchant key", func(p map[string]string) { p["sign"] = merchantSign }},
		{"MD5 downgrade", func(p map[string]string) {
			p["sign_type"] = SignTypeMD5
			filtered := ParamsFilter(p)
			keys, values := ParamsSort(filtered)
			p["sign"] = MD5String(CreateUrlString(keys, values), "")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := signedCallback(t, platformKey, now)
			tt.mutate(params)
			result, err := client.Verify(params)
			require.NoError(t, err)
			assert.False(t, result.VerifyStatus)
		})
	}

	// 平台新增的回调字段只要参与了签名，验签仍应通过。
	extended := signedCallback(t, platformKey, now)
	extended["buyer"] = "openid"
	extended["sign"] = sha256WithRSA(t, platformKey,
		"addtime=2024-07-01 16:47:32&api_trade_no=4200002345202407238253501450&buyer=openid&money=1.00&name=商品 + 50%"+
			"&out_trade_no=order-1&pid=1001&timestamp="+extended["timestamp"]+"&trade_no=2024072320222180092&trade_status=TRADE_SUCCESS&type=alipay")
	result, err = client.Verify(extended)
	require.NoError(t, err)
	assert.True(t, result.VerifyStatus)

	// 留出余量，避免测试跨秒时把边界值算进允许范围。
	outside := int64(TimestampTolerance/time.Second) + 10
	for _, offset := range []int64{-outside, outside} {
		stale := signedCallback(t, platformKey, time.Now().Unix()+offset)
		result, err = client.Verify(stale)
		assert.ErrorIs(t, err, ErrInvalidTimestamp)
		assert.Nil(t, result)
	}

	noTimestamp := map[string]string{"out_trade_no": "order-1", "trade_status": StatusTradeSuccess}
	noTimestamp["sign"] = sha256WithRSA(t, platformKey, "out_trade_no=order-1&trade_status=TRADE_SUCCESS")
	result, err = client.Verify(noTimestamp)
	assert.ErrorIs(t, err, ErrInvalidTimestamp)
	assert.Nil(t, result)

	ambiguous := signedCallback(t, platformKey, now)
	ambiguous["name"] = "item&out_trade_no=other"
	result, err = client.Verify(ambiguous)
	assert.ErrorIs(t, err, ErrInvalidParams)
	assert.Nil(t, result)
}

func TestRSAVerifyResponse(t *testing.T) {
	_, platformKey := testKeys(t)
	client := newRSAClient(t, "https://pay.example.com")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// 数字按原文、true 按 "1" 参与签名；false、null、空串、数组和对象不参与签名。
	// 接口直接返回的 pay_info 可以是带多个查询参数的链接。
	content := "code=0&money=1.00&pay_info=https://pay.example.com/pay?trade_no=1&sign=x&pay_type=jump&refundable=1&status=1&timestamp=" + ts + "&trade_no=20160806151343349"
	sign := sha256WithRSA(t, platformKey, content)
	body := `{"code":0,"msg":"","trade_no":"20160806151343349","pay_type":"jump",` +
		`"pay_info":"https://pay.example.com/pay?trade_no=1&sign=x","money":"1.00","status":1,"refundable":true,` +
		`"frozen":false,"param":null,"extra":{"a":"b"},"list":[1,2],"timestamp":"` + ts + `","sign":"` + sign + `","sign_type":"RSA"}`

	fields, err := client.VerifyResponse([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, "0", fields["code"])
	assert.Equal(t, "1", fields["status"])
	assert.Equal(t, "20160806151343349", fields["trade_no"])
	assert.Equal(t, "https://pay.example.com/pay?trade_no=1&sign=x", fields["pay_info"])
	assert.NotContains(t, fields, "extra")
	assert.NotContains(t, fields, "list")

	_, err = client.VerifyResponse([]byte(strings.Replace(body, `"status":1`, `"status":2`, 1)))
	assert.ErrorIs(t, err, ErrInvalidSignature)

	_, err = client.VerifyResponse([]byte(`{"code":-1,"msg":"订单不存在"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "订单不存在")

	_, err = client.VerifyResponse([]byte(`not json`))
	assert.Error(t, err)

	md5Client, err := NewClient(&Config{PartnerID: "1001", Key: "test-key"}, "https://pay.example.com")
	require.NoError(t, err)
	_, err = md5Client.VerifyResponse([]byte(body))
	assert.Error(t, err)
}

func TestRSAKeyFormats(t *testing.T) {
	merchantKey, platformKey := testKeys(t)
	privateDER := pkcs8(t, merchantKey)
	publicDER := pkix(t, &platformKey.PublicKey)
	wrapped := bareBase64(privateDER)
	var lines []string
	for len(wrapped) > 64 {
		lines = append(lines, wrapped[:64])
		wrapped = wrapped[64:]
	}
	lines = append(lines, wrapped)

	privateKeys := map[string]string{
		"PKCS#8 PEM":           pemText("PRIVATE KEY", privateDER),
		"PKCS#8 Base64":        bareBase64(privateDER),
		"PKCS#8 wrapped":       strings.Join(lines, "\n"),
		"PKCS#1 PEM":           pemText("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(merchantKey)),
		"PKCS#1 Base64":        bareBase64(x509.MarshalPKCS1PrivateKey(merchantKey)),
		"PEM with surrounding": "\n  " + pemText("PRIVATE KEY", privateDER) + "  \n",
	}
	for name, key := range privateKeys {
		parsed, err := ParseRSAPrivateKey(key)
		require.NoError(t, err, name)
		assert.True(t, merchantKey.Equal(parsed), name)
	}
	publicKeys := map[string]string{
		"PKIX PEM":      pemText("PUBLIC KEY", publicDER),
		"PKIX Base64":   bareBase64(publicDER),
		"PKCS#1 PEM":    pemText("RSA PUBLIC KEY", x509.MarshalPKCS1PublicKey(&platformKey.PublicKey)),
		"PKCS#1 Base64": bareBase64(x509.MarshalPKCS1PublicKey(&platformKey.PublicKey)),
	}
	for name, key := range publicKeys {
		parsed, err := ParseRSAPublicKey(key)
		require.NoError(t, err, name)
		assert.True(t, platformKey.PublicKey.Equal(parsed), name)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecPrivateDER, err := x509.MarshalPKCS8PrivateKey(ecKey)
	require.NoError(t, err)
	ecPublicDER, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	require.NoError(t, err)
	malformedKeys := map[string]string{
		"empty":          "",
		"whitespace":     " \n ",
		"not base64":     "not a key!",
		"base64 garbage": base64.StdEncoding.EncodeToString([]byte("garbage")),
		"truncated PEM":  "-----BEGIN PRIVATE KEY-----\n" + bareBase64(privateDER)[:40],
		"truncated DER":  bareBase64(privateDER[:len(privateDER)/2]),
	}
	for name, key := range malformedKeys {
		_, err := ParseRSAPrivateKey(key)
		assert.ErrorIs(t, err, ErrInvalidKey, name)
		_, err = ParseRSAPublicKey(key)
		assert.ErrorIs(t, err, ErrInvalidKey, name)
	}
	for name, key := range map[string]string{"EC private key": bareBase64(ecPrivateDER), "public key": bareBase64(publicDER)} {
		_, err := ParseRSAPrivateKey(key)
		assert.ErrorIs(t, err, ErrInvalidKey, name)
	}
	for name, key := range map[string]string{"EC public key": bareBase64(ecPublicDER), "private key": bareBase64(privateDER)} {
		_, err := ParseRSAPublicKey(key)
		assert.ErrorIs(t, err, ErrInvalidKey, name)
	}
}

func TestNewClientValidatesRSAConfig(t *testing.T) {
	merchantKey, platformKey := testKeys(t)
	privateKey := bareBase64(pkcs8(t, merchantKey))
	publicKey := bareBase64(pkix(t, &platformKey.PublicKey))

	client, err := NewClient(&Config{SignType: "rsa", MerchantPrivateKey: privateKey, PlatformPublicKey: publicKey}, "https://pay.example.com")
	require.NoError(t, err)
	assert.NotNil(t, client)

	tests := []struct {
		name   string
		config *Config
	}{
		{"missing private key", &Config{SignType: SignTypeRSA, PlatformPublicKey: publicKey}},
		{"missing public key", &Config{SignType: SignTypeRSA, MerchantPrivateKey: privateKey}},
		{"malformed private key", &Config{SignType: SignTypeRSA, MerchantPrivateKey: "MIIEv", PlatformPublicKey: publicKey}},
		{"malformed public key", &Config{SignType: SignTypeRSA, MerchantPrivateKey: privateKey, PlatformPublicKey: "-----BEGIN PUBLIC KEY-----"}},
		{"keys swapped", &Config{SignType: SignTypeRSA, MerchantPrivateKey: publicKey, PlatformPublicKey: privateKey}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config, "https://pay.example.com")
			assert.ErrorIs(t, err, ErrInvalidKey)
			assert.Nil(t, client)
		})
	}

	client, err = NewClient(&Config{SignType: "SHA1", Key: "test-key"}, "https://pay.example.com")
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestMD5SignTypeKeepsLegacyBehavior(t *testing.T) {
	for _, signType := range []string{"", SignTypeMD5, "md5"} {
		t.Run("sign type "+signType, func(t *testing.T) {
			client, err := NewClient(&Config{PartnerID: "1", Key: "test-key", SignType: signType}, "https://pay.example.com")
			require.NoError(t, err)

			paymentURL, params, err := client.Purchase(validPurchaseArgs())
			require.NoError(t, err)
			assert.Equal(t, "https://pay.example.com/submit.php", paymentURL)
			assert.Equal(t, map[string]string{
				"pid":          "1",
				"type":         "wxpay",
				"out_trade_no": "order-1",
				"notify_url":   "https://merchant.example.com/notify",
				"name":         "商品名称",
				"money":        "1.00",
				"device":       "pc",
				"return_url":   "https://merchant.example.com/return",
				"sign":         MD5String("device=pc&money=1.00&name=商品名称&notify_url=https://merchant.example.com/notify&out_trade_no=order-1&pid=1&return_url=https://merchant.example.com/return&type=wxpay", "test-key"),
				"sign_type":    SignTypeMD5,
			}, params)

			signed, err := client.SignParams(map[string]string{"out_trade_no": "order-1"})
			require.NoError(t, err)
			assert.Equal(t, MD5String("out_trade_no=order-1&pid=1", "test-key"), signed["sign"])
			assert.NotContains(t, signed, "timestamp")

			callback := map[string]string{"out_trade_no": "order-1", "trade_status": StatusTradeSuccess, "sign_type": SignTypeMD5}
			callback["sign"] = MD5String("out_trade_no=order-1&trade_status=TRADE_SUCCESS", "test-key")
			result, err := client.Verify(callback)
			require.NoError(t, err)
			assert.True(t, result.VerifyStatus)
		})
	}
}
