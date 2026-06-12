package main

import (
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/samber/lo"
)

func main() {
	baseURL := mustGetenv("EPAY_BASE_URL")
	appBaseURL := mustGetenv("APP_PUBLIC_URL")

	client, err := epay.NewClient(&epay.Config{
		PartnerID: mustGetenv("EPAY_PARTNER_ID"),
		Key:       mustGetenv("EPAY_KEY"),
	}, baseURL)
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
		if err == nil && verifyInfo.VerifyStatus {
			writer.Write([]byte("success"))
		} else {
			writer.Write([]byte("fail"))
		}

		if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
			log.Println(verifyInfo)
		}
	})
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
