package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/samber/lo"
)

func main() {
	baseURL := mustGetenv("EPAY_BASE_URL")
	appBaseURL := mustGetenv("APP_PUBLIC_URL")

	// EPAY_SIGN_TYPE 为 RSA 时使用商户私钥和平台公钥，否则使用 MD5 商户密钥。
	signType := os.Getenv("EPAY_SIGN_TYPE")
	rsaMode := strings.EqualFold(signType, epay.SignTypeRSA)
	config := &epay.Config{
		PartnerID: mustGetenv("EPAY_PARTNER_ID"),
		SignType:  signType,
	}
	if rsaMode {
		config.MerchantPrivateKey = mustGetenv("EPAY_MERCHANT_PRIVATE_KEY")
		config.PlatformPublicKey = mustGetenv("EPAY_PLATFORM_PUBLIC_KEY")
	} else {
		config.Key = mustGetenv("EPAY_KEY")
	}
	client, err := epay.NewClient(config, baseURL)
	if err != nil {
		log.Panicln(err)
	}

	callbackURL, err := url.JoinPath(appBaseURL, "verify")
	if err != nil {
		log.Panicln(err)
	}
	notify, err := url.Parse(callbackURL)
	if err != nil {
		log.Panicln(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		paymentURL, params, err := client.Purchase(&epay.PurchaseArgs{
			Type:           "wxpay",
			ServiceTradeNo: "8412317576584121",
			Name:           "test",
			Money:          "0.01",
			Device:         epay.PC,
			NotifyUrl:      notify,
			ReturnUrl:      notify,
		})
		if err != nil {
			log.Println(err)
			return
		}

		html := "<form id='alipaysubmit' name='alipaysubmit' action='" + paymentURL + "' method='POST'>"
		for key, value := range params {
			html += "<input type='hidden' name='" + key + "' value='" + value + "'/>"
		}
		html += "<input type='submit'>POST</form>"

		writer.Header().Set("Content-Type", "text/html")
		writer.Write([]byte(html))
	})
	mux.HandleFunc("/verify", func(writer http.ResponseWriter, request *http.Request) {
		params := lo.Reduce(lo.Keys(request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = request.URL.Query().Get(t)
			return r
		}, map[string]string{})

		verifyInfo, err := client.Verify(params)
		if err != nil || !verifyInfo.VerifyStatus {
			writer.Write([]byte("fail"))
			return
		}

		writer.Write([]byte("success"))
		if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
			log.Println(verifyInfo)
		}
	})
	if rsaMode {
		queryURL, err := url.JoinPath(baseURL, "api/pay/query")
		if err != nil {
			log.Panicln(err)
		}
		// 订单查询：SignParams 签名请求，VerifyResponse 校验返回数据。
		mux.HandleFunc("/query", func(writer http.ResponseWriter, request *http.Request) {
			params, err := client.SignParams(map[string]string{
				"out_trade_no": request.URL.Query().Get("out_trade_no"),
			})
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			form := url.Values{}
			for key, value := range params {
				form.Set(key, value)
			}
			resp, err := http.PostForm(queryURL, form)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadGateway)
				return
			}
			result, err := client.VerifyResponse(body)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadGateway)
				return
			}
			fmt.Fprintf(writer, "trade_no=%s status=%s money=%s\n", result["trade_no"], result["status"], result["money"])
		})
	}
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Panicln(err)
	}
}

func mustGetenv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("%s is required", key)
	}
	return value
}
