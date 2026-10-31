package controller

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/gin-gonic/gin"
)

func publicAlipayOptions(settings *pay.Settings) []*model.Option {
	if settings == nil {
		settings = &pay.Settings{Config: pay.Config{Sandbox: true}}
	}
	values := map[string]string{
		"AlipayEnabled": strconv.FormatBool(settings.Enabled), "AlipaySandbox": strconv.FormatBool(settings.Sandbox),
		"AlipayAppId": settings.AppID, "AlipaySellerId": settings.SellerID, "AlipayNotifyUrl": settings.NotifyURL,
		"AlipayReturnUrl": settings.ReturnURL, "AlipayCNYPerUSD": settings.CNYPerUSD,
	}
	options := make([]*model.Option, 0, len(values))
	for key, value := range values {
		options = append(options, &model.Option{Key: "payment_setting." + key, Value: value})
	}
	return options
}

// RootAuth is enforced by the option router. This credential operation also
// requires a dashboard session rather than a personal API access token.
func SaveAlipaySettings(c *gin.Context) {
	if c.GetBool("use_access_token") {
		c.Status(http.StatusForbidden)
		return
	}
	if os.Getenv("ALIPAY_DIRECT_CONFIG_FILE") != "" {
		common.ApiErrorT(c, "Alipay settings are managed by a private file")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	var proposed pay.Settings
	if err := common.DecodeJson(c.Request.Body, &proposed); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if proposed.Enabled && !requirePaymentCompliance(c) {
		return
	}
	settings, err := model.SaveManagedAlipaySettings(proposed)
	if err != nil {
		// No raw database or provider error is returned: ORM diagnostics can
		// contain the option's write-only private key.
		common.ApiErrorT(c, "Alipay settings could not be saved. Check the keys, callback URL and existing orders.")
		return
	}
	common.SysLog(fmt.Sprintf("Direct Alipay settings updated actor=%d enabled=%t sandbox=%t", c.GetInt("id"), settings.Enabled, settings.Sandbox))
	common.ApiSuccess(c, publicAlipayOptions(settings))
}
