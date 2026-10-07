# go-epay

`go-epay` 是一个易支付 Go SDK，提供支付参数生成、MD5 签名和回调验签能力。

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
| `EPAY_KEY` | 易支付商户密钥 |
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

## 说明

- 当前签名方式为 `MD5`。
- 生成签名时会自动过滤 `sign`、`sign_type` 和空值参数。
- 支付成功状态可使用 `epay.StatusTradeSuccess` 判断。
