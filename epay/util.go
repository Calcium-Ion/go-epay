package epay

import (
	"crypto/md5"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/samber/lo"
)

// ErrInvalidParams 表示参数无法安全地拼接为易支付待签名字符串。
var ErrInvalidParams = errors.New("epay: invalid signing parameters")

// ParamsFilter 过滤参数，生成签名时需删除 “sign” 和 “sign_type” 参数
func ParamsFilter(params map[string]string) map[string]string {
	return lo.PickBy[string, string](params, func(key string, value string) bool {
		return !(key == "sign" || key == "sign_type" || value == "")
	})
}

// ParamsSort 对参数进行排序，返回排序后的 keys 和 values （go 中 map 为乱序）
func ParamsSort(params map[string]string) ([]string, []string) {
	keys := lo.Keys(params)
	sort.Strings(keys)

	values := lo.Map(keys, func(key string, i int) string {
		return params[key]
	})

	return keys, values
}

// CreateUrlString 生成待签名字符串，非法参数返回空字符串。
//
// Deprecated: 使用 CreateUrlStringWithError 处理参数校验错误，不要对失败返回的空字符串签名。
func CreateUrlString(keys []string, values []string) string {
	urlString, _ := CreateUrlStringWithError(keys, values)
	return urlString
}

// CreateUrlStringWithError 生成待签名字符串，例如 a=d&b=e&c=f。
// 易支付协议不对值进行 URL 编码，因此拒绝会使参数边界产生歧义的分隔符。
// 参数名不得为空或包含 &、=，参数值不得包含 &；值中的 = 不影响边界。
func CreateUrlStringWithError(keys []string, values []string) (string, error) {
	if len(keys) != len(values) {
		return "", fmt.Errorf("%w: keys and values have different lengths", ErrInvalidParams)
	}
	var urlString strings.Builder
	for i, key := range keys {
		if key == "" || strings.ContainsAny(key, "&=") {
			return "", fmt.Errorf("%w: invalid parameter name %q", ErrInvalidParams, key)
		}
		if strings.Contains(values[i], "&") {
			return "", fmt.Errorf("%w: parameter %q contains '&' in its value", ErrInvalidParams, key)
		}
		if i > 0 {
			urlString.WriteByte('&')
		}
		urlString.WriteString(key)
		urlString.WriteByte('=')
		urlString.WriteString(values[i])
	}
	return urlString.String(), nil
}

// MD5String 生成 加盐(商户 key) MD5 字符串
func MD5String(urlString string, key string) string {
	digest := md5.Sum([]byte(urlString + key))
	return fmt.Sprintf("%x", digest)
}

// GenerateParams 生成加签参数，非法参数返回 nil 且不修改输入。
//
// Deprecated: 使用 GenerateParamsWithError 处理参数校验错误。
func GenerateParams(params map[string]string, key string) map[string]string {
	signedParams, _ := GenerateParamsWithError(params, key)
	return signedParams
}

// GenerateParamsWithError 校验参数并生成签名，成功时将 sign 和 sign_type 写回 params。
// 校验失败时返回 ErrInvalidParams 包装错误，且不修改输入。
func GenerateParamsWithError(params map[string]string, key string) (map[string]string, error) {
	sign, err := generateSign(params, key)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign
	params["sign_type"] = "MD5"
	return params, nil
}

func generateSign(params map[string]string, key string) (string, error) {
	if params == nil {
		return "", fmt.Errorf("%w: nil parameter map", ErrInvalidParams)
	}
	filtered := ParamsFilter(params)
	keys, values := ParamsSort(filtered)
	urlString, err := CreateUrlStringWithError(keys, values)
	if err != nil {
		return "", err
	}
	return MD5String(urlString, key), nil
}
