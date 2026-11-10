package epay

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidKey 表示 RSA 密钥缺失或格式无法解析。
	ErrInvalidKey = errors.New("epay: invalid RSA key")
	// ErrInvalidSignature 表示接口返回数据的签名校验失败。
	ErrInvalidSignature = errors.New("epay: invalid signature")
	// ErrInvalidTimestamp 表示 RSA 数据签名有效，但 timestamp 缺失或超出 TimestampTolerance。
	ErrInvalidTimestamp = errors.New("epay: invalid timestamp")
)

// TimestampTolerance RSA 回调和接口返回数据中 timestamp 与本机时间允许的最大偏差。
const TimestampTolerance = 5 * time.Minute

// ParseRSAPrivateKey 解析商户私钥。
// 支持 PEM 文本，或去掉首尾行的 Base64 文本（可含换行）；格式为 PKCS#8 或 PKCS#1。
func ParseRSAPrivateKey(key string) (*rsa.PrivateKey, error) {
	der, err := decodeKeyDER(key)
	if err != nil {
		return nil, err
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		privateKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%w: not an RSA private key", ErrInvalidKey)
		}
		return privateKey, nil
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("%w: unsupported private key format", ErrInvalidKey)
	}
	return privateKey, nil
}

// ParseRSAPublicKey 解析平台公钥。
// 支持 PEM 文本，或去掉首尾行的 Base64 文本（可含换行）；格式为 PKIX 或 PKCS#1。
func ParseRSAPublicKey(key string) (*rsa.PublicKey, error) {
	der, err := decodeKeyDER(key)
	if err != nil {
		return nil, err
	}
	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		publicKey, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: not an RSA public key", ErrInvalidKey)
		}
		return publicKey, nil
	}
	publicKey, err := x509.ParsePKCS1PublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("%w: unsupported public key format", ErrInvalidKey)
	}
	return publicKey, nil
}

func decodeKeyDER(key string) ([]byte, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("%w: empty key", ErrInvalidKey)
	}
	if strings.HasPrefix(key, "-----BEGIN") {
		block, _ := pem.Decode([]byte(key))
		if block == nil {
			return nil, fmt.Errorf("%w: malformed PEM block", ErrInvalidKey)
		}
		return block.Bytes, nil
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(key), ""))
	if err != nil {
		return nil, fmt.Errorf("%w: key is neither PEM nor Base64", ErrInvalidKey)
	}
	return der, nil
}

// rsaSignContent 生成 RSA 模式的待签名字符串：剔除 sign、sign_type 和空白值，按参数名升序用 & 拼接。
// 参数名不得为空或包含 &、=。allowAmpersandValues 为 false 时，值中的 & 与 MD5 模式一样被拒绝；
// 校验接口直接返回的数据时允许 &，因为 pay_info 等字段可能是带查询参数的链接或 HTML。
func rsaSignContent(params map[string]string, allowAmpersandValues bool) (string, error) {
	if params == nil {
		return "", fmt.Errorf("%w: nil parameter map", ErrInvalidParams)
	}
	filtered := make(map[string]string, len(params))
	for key, value := range params {
		// 只含空白字符的值视为空值，不参与签名。
		if key == "sign" || key == "sign_type" || strings.Trim(value, " \t\n\r\x00\x0b") == "" {
			continue
		}
		filtered[key] = value
	}
	keys, values := ParamsSort(filtered)
	if !allowAmpersandValues {
		return CreateUrlStringWithError(keys, values)
	}
	var content strings.Builder
	for i, key := range keys {
		if key == "" || strings.ContainsAny(key, "&=") {
			return "", fmt.Errorf("%w: invalid parameter name %q", ErrInvalidParams, key)
		}
		if i > 0 {
			content.WriteByte('&')
		}
		content.WriteString(key)
		content.WriteByte('=')
		content.WriteString(values[i])
	}
	return content.String(), nil
}

// verifyRSA 使用平台公钥校验签名，算法固定为 SHA256WithRSA，不读取数据里的 sign_type。
// 签名不匹配返回 false；签名有效但 timestamp 缺失或超出 TimestampTolerance 时返回 ErrInvalidTimestamp。
func (c *Client) verifyRSA(params map[string]string, allowAmpersandValues bool) (bool, error) {
	publicKey, err := ParseRSAPublicKey(c.Config.PlatformPublicKey)
	if err != nil {
		return false, err
	}
	content, err := rsaSignContent(params, allowAmpersandValues)
	if err != nil {
		return false, err
	}
	signature, err := base64.StdEncoding.DecodeString(params["sign"])
	if err != nil || len(signature) == 0 {
		return false, nil
	}
	digest := sha256.Sum256([]byte(content))
	if rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) != nil {
		return false, nil
	}
	timestamp, err := strconv.ParseInt(params["timestamp"], 10, 64)
	if err != nil {
		return false, fmt.Errorf("%w: %q", ErrInvalidTimestamp, params["timestamp"])
	}
	skew := time.Now().Unix() - timestamp
	tolerance := int64(TimestampTolerance / time.Second)
	if skew > tolerance || skew < -tolerance {
		return false, fmt.Errorf("%w: %q is outside the allowed window", ErrInvalidTimestamp, params["timestamp"])
	}
	return true, nil
}

// VerifyResponse 校验 RSA 模式下接口（如订单查询、退款）返回的 JSON，校验通过后以字符串返回各字段。
// 数字保留原文，true 记为 "1"，false 和 null 记为空值；数组和对象不参与签名，也不出现在结果中。
// code 不为 0 时返回包含 msg 的错误；签名不匹配返回 ErrInvalidSignature，
// timestamp 缺失或超出 TimestampTolerance 返回 ErrInvalidTimestamp。
func (c *Client) VerifyResponse(body []byte) (map[string]string, error) {
	signType, err := c.signType()
	if err != nil {
		return nil, err
	}
	if signType != SignTypeRSA {
		return nil, errors.New("epay: VerifyResponse requires the RSA sign type")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("epay: decode response: %w", err)
	}
	fields := make(map[string]string, len(raw))
	for key, value := range raw {
		switch v := value.(type) {
		case string:
			fields[key] = v
		case json.Number:
			fields[key] = v.String()
		case bool:
			if v {
				fields[key] = "1"
			} else {
				fields[key] = ""
			}
		case nil:
			fields[key] = ""
		}
	}
	if fields["code"] != "0" {
		return nil, fmt.Errorf("epay: request failed with code %s: %s", fields["code"], fields["msg"])
	}
	verified, err := c.verifyRSA(fields, true)
	if err != nil {
		return nil, err
	}
	if !verified {
		return nil, ErrInvalidSignature
	}
	return fields, nil
}
