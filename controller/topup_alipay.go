package controller

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
)

type directAlipaySettings struct {
	appID      string
	privateKey string
	publicKey  string
	notifyURL  string
	returnURL  string
}

func directAlipaySettingsFromOptions(options map[string]string) directAlipaySettings {
	return directAlipaySettings{
		appID:      strings.TrimSpace(options["AlipayAppID"]),
		privateKey: strings.TrimSpace(options["AlipayPrivateKey"]),
		publicKey:  strings.TrimSpace(options["AlipayPublicKey"]),
		notifyURL:  strings.TrimSpace(options["AlipayNotifyURL"]),
		returnURL:  strings.TrimSpace(options["AlipayReturnURL"]),
	}
}

func (settings directAlipaySettings) verifier() alipayVerifier {
	return alipayVerifier{AppID: settings.appID, PublicKey: settings.publicKey}
}

func (settings directAlipaySettings) configured() bool {
	return settings.appID != "" && settings.privateKey != "" && settings.publicKey != ""
}

func isDirectAlipayTopUpEnabled() bool {
	if model.DB == nil || !isPaymentComplianceConfirmed() || !operation_setting.ContainsPayMethod("alipay") {
		return false
	}
	options, err := model.GetDirectAlipayOptions()
	return err == nil && directAlipaySettingsFromOptions(options).configured()
}

func (settings directAlipaySettings) client() (*alipay.Client, error) {
	if !settings.configured() {
		return nil, fmt.Errorf("direct Alipay is not configured")
	}
	client, err := alipay.New(settings.appID, settings.privateKey, true)
	if err != nil {
		return nil, fmt.Errorf("initialize Alipay client: %w", err)
	}
	if err := client.LoadAliPayPublicKey(settings.publicKey); err != nil {
		return nil, fmt.Errorf("load Alipay public key: %w", err)
	}
	return client, nil
}

func requestAlipay(c *gin.Context, req EpayRequest, settings directAlipaySettings) {
	if req.Amount < getMinTopup() {
		paymentError(c, common.NewMessage("Top-up amount cannot be less than {{min}}", map[string]any{"min": getMinTopup()}))
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		paymentError(c, common.NewMessage("Failed to get the user group"))
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		paymentError(c, common.NewMessage("Payment amount is too low"))
		return
	}
	client, err := settings.client()
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("direct Alipay is unavailable: %q", err.Error()))
		paymentError(c, common.NewMessage("Payment information has not been configured by administrator"))
		return
	}
	tradeNo := "ALIPAYUSR" + rand.Text()[:20] + "_" + settings.verifier().id()
	callback := settings.notifyURL
	if strings.TrimSpace(callback) == "" {
		callback = service.GetCallbackAddress() + "/api/user/alipay/notify"
	}
	returnURL := settings.returnURL
	if strings.TrimSpace(returnURL) == "" {
		returnURL = service.GetCallbackAddress() + paymentReturnPath("/usage-logs")
	}
	request := alipay.TradePreCreate{}
	request.Trade.OutTradeNo = tradeNo
	request.Trade.TotalAmount = strconv.FormatFloat(payMoney, 'f', 2, 64)
	request.Trade.Subject = fmt.Sprintf("TUC%d", req.Amount)
	request.Trade.ProductCode = "FACE_TO_FACE_PAYMENT"
	request.Trade.NotifyURL = callback
	response, err := client.TradePreCreate(context.Background(), request)
	if err != nil || response == nil || response.IsFailure() || strings.TrimSpace(response.QRCode) == "" {
		logger.LogError(c.Request.Context(), common.LogText("Alipay precreate failed trade_no=%s error=%q", tradeNo, err))
		paymentError(c, common.NewMessage("Failed to start payment"))
		return
	}
	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	if err := (&model.TopUp{UserId: id, Amount: amount, Money: payMoney, TradeNo: tradeNo, PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderAlipay, CreateTime: time.Now().Unix(), Status: common.TopUpStatusPending}).Insert(); err != nil {
		logger.LogError(c.Request.Context(), common.LogText("Alipay order insert failed trade_no=%s error=%q", tradeNo, err))
		paymentError(c, common.NewMessage("Failed to create order"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"trade_no": tradeNo, "qr_code": response.QRCode, "amount": fmt.Sprintf("%.2f", payMoney), "return_url": returnURL}, "url": "/api/user/alipay/checkout"})
}

func AlipayCheckout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	common.ApiErrorStatus(c, http.StatusGone, common.NewMessage("Open the wallet to pay with Alipay."))
}

func AlipayNotify(c *gin.Context) {
	var values url.Values
	if c.Request.Method == http.MethodPost {
		if err := c.Request.ParseForm(); err != nil {
			_, _ = io.WriteString(c.Writer, "fail")
			return
		}
		values = c.Request.PostForm
	} else {
		values = c.Request.URL.Query()
	}
	for _, entries := range values {
		if len(entries) != 1 {
			_, _ = io.WriteString(c.Writer, "fail")
			return
		}
	}
	tradeNo := strings.TrimSpace(values.Get("out_trade_no"))
	if tradeNo == "" {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	order := model.GetTopUpByTradeNo(tradeNo)
	if order == nil || order.PaymentProvider != model.PaymentProviderAlipay {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	options, err := model.GetDirectAlipayOptions()
	if err != nil {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	verifier, err := alipayVerifierForOrder(options, tradeNo)
	if err != nil || values.Get("app_id") != verifier.AppID {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	client, err := verifier.decoder()
	if err != nil {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	values.Del("alipay_cert_sn")
	notification, err := client.DecodeNotification(c.Request.Context(), values)
	if err != nil {
		current := directAlipaySettingsFromOptions(options).verifier()
		if current.AppID == verifier.AppID && current.PublicKey != verifier.PublicKey {
			if decoder, initErr := current.decoder(); initErr == nil {
				notification, err = decoder.DecodeNotification(c.Request.Context(), values)
			}
		}
	}
	if err != nil || notification == nil || notification.AppId != verifier.AppID || notification.OutTradeNo != order.TradeNo || notification.TradeNo == "" || (notification.TradeStatus != alipay.TradeStatusSuccess && notification.TradeStatus != alipay.TradeStatusFinished) {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	actual, err := decimal.NewFromString(notification.TotalAmount)
	expected := decimal.NewFromFloat(order.Money).Round(2)
	if err != nil || actual.Cmp(expected) != 0 {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	if _, err := model.RechargeAlipay(tradeNo, "alipay", c.ClientIP()); err != nil {
		if !errors.Is(err, model.ErrTopUpStatusInvalid) {
			logger.LogError(c.Request.Context(), common.LogText("Alipay settlement failed trade_no=%s error=%q", tradeNo, err))
		}
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	_, _ = io.WriteString(c.Writer, "success")
}
