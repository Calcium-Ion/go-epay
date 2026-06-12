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
