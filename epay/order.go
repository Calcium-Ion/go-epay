package epay

import (
	"crypto/subtle"
	"net/url"
	"path"

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
	PurchaseUrl = "/submit.php"
)

// Purchase 生成支付链接和参数
func (c *Client) Purchase(args *PurchaseArgs) (string, map[string]string, error) {
	requestParams := map[string]string{
		"pid":          c.Config.PartnerID,
		"type":         args.Type,
		"out_trade_no": args.ServiceTradeNo,
		"notify_url":   args.NotifyUrl.String(),
		"name":         args.Name,
		"money":        args.Money,
		"device":       string(args.Device),
		"sign_type":    "MD5",
		"return_url":   args.ReturnUrl.String(),
		"sign":         "",
	}

	u, err := url.Parse(c.BaseUrl.String())
	if err != nil {
		return "", nil, err
	}
	u.Path = path.Join(u.Path, PurchaseUrl)

	signedParams, err := GenerateParamsWithError(requestParams, c.Config.Key)
	if err != nil {
		return "", nil, err
	}
	return u.String(), signedParams, nil
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

func (c *Client) Verify(params map[string]string) (*VerifyRes, error) {
	expectedSign, err := generateSign(params, c.Config.Key)
	if err != nil {
		return nil, err
	}
	var verifyRes VerifyRes
	// 从 map 映射到 struct 上
	if err := mapstructure.Decode(params, &verifyRes); err != nil {
		return nil, err
	}
	// 验签只读取输入，避免将错误签名覆盖为正确签名。
	verifyRes.VerifyStatus = subtle.ConstantTimeCompare([]byte(params["sign"]), []byte(expectedSign)) == 1
	return &verifyRes, nil
}
