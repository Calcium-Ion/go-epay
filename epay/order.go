package epay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/mitchellh/mapstructure"
)

type DeviceType string

var (
	PC     DeviceType = "pc"     // PC PC端
	MOBILE DeviceType = "mobile" // MOBILE 移动端
)

type PurchaseArgs struct {
	// 支付类型
	Type string
	// 商家订单号
	ServiceTradeNo string
	// 商品名称
	Name string
	// 金额
	Money string
	// 设备类型
	Device    DeviceType
	NotifyUrl *url.URL
	ReturnUrl *url.URL
}

const (
	// PurchaseUrl MD5 模式的页面跳转支付地址。
	PurchaseUrl = "/submit.php"
	// PurchaseRSAUrl RSA 模式的页面跳转支付地址。
	PurchaseRSAUrl = "/api/pay/submit"
)

// Purchase 生成支付链接和参数
func (c *Client) Purchase(args *PurchaseArgs) (string, map[string]string, error) {
	signType, err := c.signType()
	if err != nil {
		return "", nil, err
	}
	requestParams := map[string]string{
		"type":         args.Type,
		"out_trade_no": args.ServiceTradeNo,
		"notify_url":   args.NotifyUrl.String(),
		"name":         args.Name,
		"money":        args.Money,
		"device":       string(args.Device),
		"return_url":   args.ReturnUrl.String(),
	}

	u, err := url.Parse(c.BaseUrl.String())
	if err != nil {
		return "", nil, err
	}
	purchasePath := PurchaseUrl
	if signType == SignTypeRSA {
		purchasePath = PurchaseRSAUrl
	}
	u.Path = path.Join(u.Path, purchasePath)

	signedParams, err := c.SignParams(requestParams)
	if err != nil {
		return "", nil, err
	}
	return u.String(), signedParams, nil
}

// SignParams 为请求参数签名，返回写入 pid、sign 和 sign_type 的新参数，不修改输入。
// RSA 模式还会写入当前 timestamp，并用商户私钥计算 SHA256WithRSA 签名；
// 订单查询、退款等接口请求可用它签名，再用 VerifyResponse 校验返回数据。
func (c *Client) SignParams(params map[string]string) (map[string]string, error) {
	signType, err := c.signType()
	if err != nil {
		return nil, err
	}
	if params == nil {
		return nil, fmt.Errorf("%w: nil parameter map", ErrInvalidParams)
	}
	signed := make(map[string]string, len(params)+4)
	for key, value := range params {
		signed[key] = value
	}
	signed["pid"] = c.Config.PartnerID
	if signType == SignTypeMD5 {
		return GenerateParamsWithError(signed, c.Config.Key)
	}

	privateKey, err := ParseRSAPrivateKey(c.Config.MerchantPrivateKey)
	if err != nil {
		return nil, err
	}
	signed["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	content, err := rsaSignContent(signed, false)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return nil, err
	}
	signed["sign"] = base64.StdEncoding.EncodeToString(signature)
	signed["sign_type"] = SignTypeRSA
	return signed, nil
}

const StatusTradeSuccess = "TRADE_SUCCESS"

type VerifyRes struct {
	// 支付类型
	Type string
	// 易支付订单号
	TradeNo string `mapstructure:"trade_no"`
	// 商家订单号
	ServiceTradeNo string `mapstructure:"out_trade_no"`
	// 商品名称
	Name string
	// 金额
	Money string
	// 订单支付状态
	TradeStatus string `mapstructure:"trade_status"`
	// 签名检验
	VerifyStatus bool `mapstructure:"-"`
}

// Verify 校验回调参数，不修改输入。签名不匹配时 VerifyStatus 为 false。
// RSA 模式用平台公钥验签；签名有效但 timestamp 缺失或超出 TimestampTolerance 时返回 ErrInvalidTimestamp。
func (c *Client) Verify(params map[string]string) (*VerifyRes, error) {
	signType, err := c.signType()
	if err != nil {
		return nil, err
	}
	var verified bool
	if signType == SignTypeRSA {
		verified, err = c.verifyRSA(params, false)
		if err != nil {
			return nil, err
		}
	} else {
		expectedSign, err := generateSign(params, c.Config.Key)
		if err != nil {
			return nil, err
		}
		// 验签只读取输入，避免将错误签名覆盖为正确签名。
		verified = subtle.ConstantTimeCompare([]byte(params["sign"]), []byte(expectedSign)) == 1
	}
	var verifyRes VerifyRes
	// 从 map 映射到 struct 上
	if err := mapstructure.Decode(params, &verifyRes); err != nil {
		return nil, err
	}
	verifyRes.VerifyStatus = verified
	return &verifyRes, nil
}
