package epay

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	// SignTypeMD5 使用商户密钥计算 MD5 签名，Config.SignType 为空时默认使用。
	SignTypeMD5 = "MD5"
	// SignTypeRSA 使用商户私钥签名、平台公钥验签，算法为 SHA256WithRSA。
	SignTypeRSA = "RSA"
)

type Config struct {
	PartnerID string
	// Key MD5 模式使用的商户密钥。
	Key string
	// SignType 签名方式：SignTypeMD5（默认）或 SignTypeRSA，不区分大小写。
	SignType string
	// MerchantPrivateKey RSA 模式使用的商户私钥，用于给请求签名。
	// 支持 PEM 文本，或去掉首尾行的 Base64 文本；格式为 PKCS#8 或 PKCS#1。
	MerchantPrivateKey string
	// PlatformPublicKey RSA 模式使用的平台公钥，用于校验回调和接口返回数据。
	// 支持 PEM 文本，或去掉首尾行的 Base64 文本；格式为 PKIX 或 PKCS#1。
	PlatformPublicKey string
}

var _ Service = (*Client)(nil)

// Service 易支付API
type Service interface {
	// Purchase 生成支付链接和参数
	Purchase(args *PurchaseArgs) (string, map[string]string, error)
	// Verify 验证回调参数是否符合签名
	Verify(params map[string]string) (*VerifyRes, error)
}

type Client struct {
	Config  *Config
	BaseUrl *url.URL
}

// NewClient 创建一个新的易支付客户端。
// RSA 模式会解析商户私钥和平台公钥，密钥缺失或格式错误时返回 ErrInvalidKey 包装错误。
func NewClient(config *Config, baseUrl string) (*Client, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}
	client := &Client{
		Config:  config,
		BaseUrl: u,
	}
	signType, err := client.signType()
	if err != nil {
		return nil, err
	}
	if signType == SignTypeRSA {
		if _, err := ParseRSAPrivateKey(config.MerchantPrivateKey); err != nil {
			return nil, fmt.Errorf("merchant private key: %w", err)
		}
		if _, err := ParseRSAPublicKey(config.PlatformPublicKey); err != nil {
			return nil, fmt.Errorf("platform public key: %w", err)
		}
	}
	return client, nil
}

func (c *Client) signType() (string, error) {
	if c.Config == nil || c.Config.SignType == "" {
		return SignTypeMD5, nil
	}
	signType := strings.ToUpper(c.Config.SignType)
	if signType != SignTypeMD5 && signType != SignTypeRSA {
		return "", fmt.Errorf("epay: unsupported sign type %q", c.Config.SignType)
	}
	return signType, nil
}
