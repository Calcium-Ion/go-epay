# go-epay

`go-epay` 是一个易支付 Go SDK，提供支付参数生成、MD5 / RSA 签名和回调验签能力。

## 安装

```bash
go get github.com/Calcium-Ion/go-epay
```

## 快速开始

```go
package main

import (
	"log"
	"net/url"
	"os"

	"github.com/Calcium-Ion/go-epay/epay"
)

func main() {
	client, err := epay.NewClient(&epay.Config{
		PartnerID: os.Getenv("EPAY_PARTNER_ID"),
		Key:       os.Getenv("EPAY_KEY"),
	}, os.Getenv("EPAY_BASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	notifyURL, err := url.Parse(os.Getenv("EPAY_NOTIFY_URL"))
	if err != nil {
		log.Fatal(err)
	}

	returnURL, err := url.Parse(os.Getenv("EPAY_RETURN_URL"))
	if err != nil {
		log.Fatal(err)
	}

	paymentURL, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           "wxpay",
		ServiceTradeNo: "order-no",
		Name:           "商品名称",
		Money:          "0.01",
		Device:         epay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println(paymentURL, params)
}
```

`Purchase` 返回支付提交地址和已签名参数。业务方可以将参数渲染为表单，也可以按自己的网关接入方式提交。

## 回调验签

```go
verifyRes, err := client.Verify(params)
if err != nil {
	return err
}

if verifyRes.VerifyStatus && verifyRes.TradeStatus == epay.StatusTradeSuccess {
	// 处理支付成功订单
}
```

`Verify` 不会修改传入的参数。参数包含歧义分隔符时返回错误；签名不匹配时，`VerifyStatus` 为 `false`。

## RSA 签名

易支付的 RSA 模式使用商户私钥给请求签名，使用平台公钥校验异步通知、同步跳转和接口返回数据，算法为 SHA256WithRSA。两把密钥都在易支付商户后台获取。设置 `SignType` 为 `epay.SignTypeRSA` 即可启用，其余调用方式不变：

```go
client, err := epay.NewClient(&epay.Config{
	PartnerID:          os.Getenv("EPAY_PARTNER_ID"),
	SignType:           epay.SignTypeRSA,
	MerchantPrivateKey: os.Getenv("EPAY_MERCHANT_PRIVATE_KEY"),
	PlatformPublicKey:  os.Getenv("EPAY_PLATFORM_PUBLIC_KEY"),
}, os.Getenv("EPAY_BASE_URL"))
if err != nil {
	// 密钥缺失或格式错误时，errors.Is(err, epay.ErrInvalidKey) 为 true
	return err
}
```

密钥可以是 PEM 文本，也可以是去掉首尾两行的 Base64 文本，中间有无换行都可以。商户私钥支持 PKCS#8（`BEGIN PRIVATE KEY`）和 PKCS#1（`BEGIN RSA PRIVATE KEY`），平台公钥支持 PKIX（`BEGIN PUBLIC KEY`）和 PKCS#1（`BEGIN RSA PUBLIC KEY`）。例如：

```bash
export EPAY_SIGN_TYPE="RSA"
# 直接粘贴商户后台显示的单行 Base64 文本
export EPAY_MERCHANT_PRIVATE_KEY="<商户私钥>"
export EPAY_PLATFORM_PUBLIC_KEY="<平台公钥>"
# 或从 PEM 文件读取
export EPAY_MERCHANT_PRIVATE_KEY="$(cat merchant_private_key.pem)"
```

商户私钥只用于签名，不要提交到代码仓库或发给他人。

RSA 模式下：

- `Purchase` 返回 `/api/pay/submit` 提交地址，参数中加入当前 10 位秒级 `timestamp`，`sign_type` 为 `RSA`。平台会校验时间戳，请在用户发起支付时生成参数，不要提前生成后缓存。
- `Verify` 用平台公钥验签，不按回调中的 `sign_type` 切换算法。签名不匹配时 `VerifyStatus` 为 `false`；签名有效但 `timestamp` 缺失或与本机时间相差超过 5 分钟（`epay.TimestampTolerance`）时，返回可通过 `errors.Is(err, epay.ErrInvalidTimestamp)` 判断的错误，请保持服务器时间准确。
- 待签名字符串的规则与 MD5 模式相同：剔除 `sign`、`sign_type` 和空值（只含空白字符的值也视为空值），按参数名 ASCII 升序拼接为 `key=value`，用 `&` 连接，值不做 URL 编码。下文的签名参数限制同样适用。

订单查询、退款等接口用 `SignParams` 签名请求，用 `VerifyResponse` 校验返回的 JSON：

```go
params, err := client.SignParams(map[string]string{"out_trade_no": "order-no"})
if err != nil {
	return err
}
form := url.Values{}
for key, value := range params {
	form.Set(key, value)
}
queryURL, err := url.JoinPath(os.Getenv("EPAY_BASE_URL"), "api/pay/query")
if err != nil {
	return err
}
resp, err := http.PostForm(queryURL, form)
if err != nil {
	return err
}
defer resp.Body.Close()
body, err := io.ReadAll(resp.Body)
if err != nil {
	return err
}
result, err := client.VerifyResponse(body)
if err != nil {
	return err
}
if result["status"] == "1" {
	// 订单已支付
}
```

`SignParams` 会写入 `pid`、`sign`、`sign_type`，RSA 模式还会写入 `timestamp`，不修改传入的参数；MD5 模式也可以使用。`VerifyResponse` 只用于 RSA 模式：`code` 不为 0 时返回包含 `msg` 的错误，签名不匹配时返回 `epay.ErrInvalidSignature`，时间戳校验与 `Verify` 相同。校验通过后以字符串返回各字段，数字保留原文，数组和对象字段不参与签名，也不出现在结果中。接口直接返回的数据允许值中包含 `&`，例如带多个查询参数的支付链接。

## 签名参数限制

易支付 [MD5 签名协议](https://pay.yzfpay.com/doc/v1_legacy_api.html) 要求按参数名排序后拼接原始值，不对值做 URL 编码。本 SDK 保留这一格式，并在签名和验签前拒绝会造成参数边界歧义的输入：

- 参与签名的参数名不得为空，也不得包含 `&` 或 `=`。
- 参与签名的参数值不得包含 `&`，包括商品名称、订单号、`notify_url` 和 `return_url`。
- 值中的 `=`、空格、中文、`+` 和字面量 `%26` 保持原样；SDK 不会额外解码或转义它们。

例如，`return_url=https://example.com/return?a=1&b=2` 会被拒绝。回调地址请使用路径或单个 query 参数承载业务状态。不要将整个 URL 编码来绕过限制，否则可能改变网关跳转地址。

`Purchase`、`Verify` 会返回可通过 `errors.Is(err, epay.ErrInvalidParams)` 判断的错误。自行生成签名时，请使用支持错误返回的接口：

```go
signedParams, err := epay.GenerateParamsWithError(params, merchantKey)
if err != nil {
	return err
}
// 使用 signedParams 提交支付请求
```

旧接口 `GenerateParams` 保留函数签名，但非法输入返回 `nil`，不修改原参数；请迁移到 `GenerateParamsWithError`。旧接口 `CreateUrlString` 对非法输入返回空字符串，请迁移到 `CreateUrlStringWithError`，不要对失败返回的空字符串继续计算签名。

此校验不会使修复前已签发的歧义签名失效；升级后应停止继续使用这类历史支付参数。

## 配置项

| 名称 | 说明 |
| --- | --- |
| `EPAY_PARTNER_ID` | 易支付商户 ID |
| `EPAY_SIGN_TYPE` | 签名方式，`MD5`（默认）或 `RSA` |
| `EPAY_KEY` | 易支付商户密钥，MD5 模式使用 |
| `EPAY_MERCHANT_PRIVATE_KEY` | 商户私钥，RSA 模式使用 |
| `EPAY_PLATFORM_PUBLIC_KEY` | 平台公钥，RSA 模式使用 |
| `EPAY_BASE_URL` | 易支付网关地址 |
| `EPAY_NOTIFY_URL` | 异步通知回调地址 |
| `EPAY_RETURN_URL` | 同步跳转回调地址 |

## 示例

示例程序位于 `examples/main.go`。运行前设置易支付网关和应用公开访问地址：

```bash
export EPAY_PARTNER_ID="<商户 ID>"
export EPAY_KEY="<商户密钥>"
export EPAY_BASE_URL="<易支付网关地址>"
export APP_PUBLIC_URL="<应用公开访问地址>"
go run ./examples
```

使用 RSA 模式时，将 `EPAY_KEY` 换成密钥配置。示例还会提供 `/query?out_trade_no=<商户订单号>`，演示订单查询的签名与验签：

```bash
export EPAY_PARTNER_ID="<商户 ID>"
export EPAY_SIGN_TYPE="RSA"
export EPAY_MERCHANT_PRIVATE_KEY="<商户私钥>"
export EPAY_PLATFORM_PUBLIC_KEY="<平台公钥>"
export EPAY_BASE_URL="<易支付网关地址>"
export APP_PUBLIC_URL="<应用公开访问地址>"
go run ./examples
```

## 说明

- 支持 `MD5` 和 `RSA` 两种签名方式，未设置 `SignType` 时使用 `MD5`。
- 生成签名时会自动过滤 `sign`、`sign_type` 和空值参数。
- 支付成功状态可使用 `epay.StatusTradeSuccess` 判断。
