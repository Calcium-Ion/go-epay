package epay

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateParamsRejectsAmbiguousParameters(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]string
	}{
		{"value separator", map[string]string{"name": "item&out_trade_no=other"}},
		{"literal ampersand", map[string]string{"name": "A&B"}},
		{"key separator", map[string]string{"name&out_trade_no": "other"}},
		{"key equals", map[string]string{"name=other": "item"}},
		{"empty key", map[string]string{"": "item"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.params["sign"] = "original-sign"
			tt.params["sign_type"] = "original-type"
			before := copyParams(tt.params)
			params, err := GenerateParamsWithError(tt.params, "test-key")
			assert.ErrorIs(t, err, ErrInvalidParams)
			assert.Nil(t, params)
			assert.Equal(t, before, tt.params)
			assert.Nil(t, GenerateParams(tt.params, "test-key"))
			assert.Equal(t, before, tt.params, "rejected input must not be mutated")
			client := &Client{Config: &Config{Key: "test-key"}}
			result, err := client.Verify(tt.params)
			assert.ErrorIs(t, err, ErrInvalidParams)
			assert.Nil(t, result)
			assert.Equal(t, before, tt.params)
		})
	}
}

func TestVerifyRejectsAmbiguousCallback(t *testing.T) {
	// These distinct maps produced the same signing string before validation.
	first := map[string]string{
		"money":        "1.00",
		"name":         "item&out_trade_no=other",
		"out_trade_no": "order",
		"trade_status": StatusTradeSuccess,
	}
	second := map[string]string{
		"money":        "1.00",
		"name":         "item",
		"out_trade_no": "other&out_trade_no=order",
		"trade_status": StatusTradeSuccess,
	}
	sign := MD5String("money=1.00&name=item&out_trade_no=other&out_trade_no=order&trade_status=TRADE_SUCCESS", "test-key")
	client := &Client{Config: &Config{Key: "test-key"}}
	for _, params := range []map[string]string{first, second} {
		params["sign"] = sign
		params["sign_type"] = "MD5"
		before := copyParams(params)
		result, err := client.Verify(params)
		assert.ErrorIs(t, err, ErrInvalidParams)
		assert.Nil(t, result)
		assert.Equal(t, before, params)
	}
}

func TestPurchaseRejectsCallbackParameterInjection(t *testing.T) {
	client, err := NewClient(&Config{PartnerID: "1", Key: "test-key"}, "https://pay.example.com")
	require.NoError(t, err)
	tests := []struct {
		name   string
		mutate func(*PurchaseArgs)
	}{
		{"return URL", func(args *PurchaseArgs) {
			args.ReturnUrl.RawQuery = "next=/&trade_no=fake&trade_status=TRADE_SUCCESS"
		}},
		{"notify URL", func(args *PurchaseArgs) {
			args.NotifyUrl.RawQuery = "a=1&b=2"
		}},
		{"product name", func(args *PurchaseArgs) {
			args.Name = "item&out_trade_no=other"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := validPurchaseArgs()
			tt.mutate(args)
			paymentURL, params, err := client.Purchase(args)
			assert.ErrorIs(t, err, ErrInvalidParams)
			assert.Empty(t, paymentURL)
			assert.Nil(t, params)
		})
	}
}

func TestVerifyDoesNotRewriteInvalidSignature(t *testing.T) {
	client := &Client{Config: &Config{Key: "test-key"}}
	params := map[string]string{"money": "1.00", "sign": "invalid", "sign_type": "MD5"}
	before := copyParams(params)
	for i := 0; i < 2; i++ {
		result, err := client.Verify(params)
		require.NoError(t, err)
		assert.False(t, result.VerifyStatus)
		assert.Equal(t, before, params)
	}
}

func TestCreateUrlStringWithError(t *testing.T) {
	tests := []struct {
		name    string
		keys    []string
		values  []string
		want    string
		wantErr bool
	}{
		{"empty", nil, nil, "", false},
		{"legacy", []string{"device", "money"}, []string{"devicev", "moneyv"}, "device=devicev&money=moneyv", false},
		{"literal characters", []string{"name"}, []string{"商品 + 50% = x%26y"}, "name=商品 + 50% = x%26y", false},
		{"single query parameter", []string{"return_url"}, []string{"https://example.com/?state=a=b"}, "return_url=https://example.com/?state=a=b", false},
		{"value separator", []string{"name"}, []string{"item&money=1"}, "", true},
		{"key separator", []string{"name&money"}, []string{"1"}, "", true},
		{"key equals", []string{"name=item"}, []string{"other"}, "", true},
		{"empty key", []string{""}, []string{"item"}, "", true},
		{"missing value", []string{"name"}, nil, "", true},
		{"extra value", nil, []string{"item"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CreateUrlStringWithError(tt.keys, tt.values)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidParams)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want, CreateUrlString(tt.keys, tt.values))
		})
	}
}

func TestSigningPreservesLegacyProtocol(t *testing.T) {
	params, err := GenerateParamsWithError(map[string]string{
		"money": "moneyv", "device": "devicev", "empty": "", "sign": "ignored&value", "sign_type": "ignored&type",
	}, "1234567")
	require.NoError(t, err)
	assert.Equal(t, "3854cc9f022e0fb821bd2e002260245d", params["sign"])
	assert.Equal(t, "MD5", params["sign_type"])
	assert.Equal(t, params, GenerateParams(copyParams(params), "1234567"))

	callback := map[string]string{
		"money": "1.00", "name": "商品 + 50% = x%26y", "out_trade_no": "order-1",
		"pid": "1", "trade_no": "epay-1", "trade_status": StatusTradeSuccess, "type": "wxpay",
		"sign_type": "MD5",
	}
	callback["sign"] = MD5String("money=1.00&name=商品 + 50% = x%26y&out_trade_no=order-1&pid=1&trade_no=epay-1&trade_status=TRADE_SUCCESS&type=wxpay", "test-key")
	before := copyParams(callback)
	client := &Client{Config: &Config{Key: "test-key"}}
	result, err := client.Verify(callback)
	require.NoError(t, err)
	assert.True(t, result.VerifyStatus)
	assert.Equal(t, callback["name"], result.Name)
	assert.Equal(t, "order-1", result.ServiceTradeNo)
	assert.Equal(t, "epay-1", result.TradeNo)
	assert.Equal(t, StatusTradeSuccess, result.TradeStatus)
	assert.Equal(t, before, callback)

	callback["money"] = "100.00"
	result, err = client.Verify(callback)
	require.NoError(t, err)
	assert.False(t, result.VerifyStatus)
	delete(callback, "sign")
	result, err = client.Verify(callback)
	require.NoError(t, err)
	assert.False(t, result.VerifyStatus)
	assert.NotContains(t, callback, "sign")
}

func TestPurchasePreservesLegacyProtocol(t *testing.T) {
	client, err := NewClient(&Config{PartnerID: "1", Key: "test-key"}, "https://pay.example.com/gateway/")
	require.NoError(t, err)
	args := validPurchaseArgs()
	args.ReturnUrl.RawQuery = "state=a%26b"
	paymentURL, params, err := client.Purchase(args)
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/gateway/submit.php", paymentURL)
	assert.Equal(t, "https://merchant.example.com/return?state=a%26b", params["return_url"])
	wantSign := MD5String("device=pc&money=1.00&name=商品名称&notify_url=https://merchant.example.com/notify&out_trade_no=order-1&pid=1&return_url=https://merchant.example.com/return?state=a%26b&type=wxpay", "test-key")
	assert.Equal(t, wantSign, params["sign"])
	assert.Equal(t, "MD5", params["sign_type"])
}

func TestSigningRejectsNilParams(t *testing.T) {
	params, err := GenerateParamsWithError(nil, "test-key")
	assert.ErrorIs(t, err, ErrInvalidParams)
	assert.Nil(t, params)
	assert.Nil(t, GenerateParams(nil, "test-key"))
	client := &Client{Config: &Config{Key: "test-key"}}
	result, err := client.Verify(nil)
	assert.ErrorIs(t, err, ErrInvalidParams)
	assert.Nil(t, result)
}

func TestVerifyRejectsURLDecodedSeparator(t *testing.T) {
	query, err := url.ParseQuery("name=item%26out_trade_no%3Dother&out_trade_no=order&sign=invalid")
	require.NoError(t, err)
	params := make(map[string]string, len(query))
	for key, values := range query {
		params[key] = values[0]
	}
	client := &Client{Config: &Config{Key: "test-key"}}
	result, err := client.Verify(params)
	assert.ErrorIs(t, err, ErrInvalidParams)
	assert.Nil(t, result)
}

func validPurchaseArgs() *PurchaseArgs {
	return &PurchaseArgs{
		Type:           "wxpay",
		ServiceTradeNo: "order-1",
		Name:           "商品名称",
		Money:          "1.00",
		Device:         PC,
		NotifyUrl:      &url.URL{Scheme: "https", Host: "merchant.example.com", Path: "/notify"},
		ReturnUrl:      &url.URL{Scheme: "https", Host: "merchant.example.com", Path: "/return"},
	}
}

func copyParams(params map[string]string) map[string]string {
	copy := make(map[string]string, len(params))
	for key, value := range params {
		copy[key] = value
	}
	return copy
}
