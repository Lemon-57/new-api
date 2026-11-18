package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"image/png"
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
	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
)

const alipayOrderTTL = 15 * time.Minute

func directAlipayConfigured() bool {
	return strings.TrimSpace(operation_setting.AlipayAppID) != "" &&
		strings.TrimSpace(operation_setting.AlipayPrivateKey) != "" &&
		strings.TrimSpace(operation_setting.AlipayPublicKey) != ""
}

func getDirectAlipayClient() (*alipay.Client, error) {
	if !directAlipayConfigured() {
		return nil, fmt.Errorf("direct Alipay is not configured")
	}
	client, err := alipay.New(operation_setting.AlipayAppID, operation_setting.AlipayPrivateKey, true)
	if err != nil {
		return nil, fmt.Errorf("initialize Alipay client: %w", err)
	}
	if err := client.LoadAliPayPublicKey(operation_setting.AlipayPublicKey); err != nil {
		return nil, fmt.Errorf("load Alipay public key: %w", err)
	}
	return client, nil
}

// RequestAlipay creates a native Alipay face-to-face precreate order. The
// browser is sent to an in-process checkout page that renders the QR code;
// payment confirmation still arrives only through the verified callback.
func RequestAlipay(c *gin.Context) {
	var req EpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	requestAlipay(c, req)
}

func requestAlipay(c *gin.Context, req EpayRequest) {
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
	client, err := getDirectAlipayClient()
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("direct Alipay is unavailable: %q", err.Error()))
		paymentError(c, common.NewMessage("Payment information has not been configured by administrator"))
		return
	}
	tradeNo := fmt.Sprintf("ALIPAYUSR%d%s%d", id, common.GetRandomString(6), time.Now().Unix())
	callback := operation_setting.AlipayNotifyURL
	if strings.TrimSpace(callback) == "" {
		callback = service.GetCallbackAddress() + "/api/user/alipay/notify"
	}
	returnURL := operation_setting.AlipayReturnURL
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
	// The frontend's existing form submitter posts the returned fields to this
	// route, so no frontend payment protocol change is needed.
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"trade_no": tradeNo, "qr_code": response.QRCode, "amount": fmt.Sprintf("%.2f", payMoney), "return_url": returnURL}, "url": "/api/user/alipay/checkout"})
}

func AlipayCheckout(c *gin.Context) {
	if !directAlipayConfigured() {
		http.Error(c.Writer, "payment unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = c.Request.ParseForm()
	tradeNo := strings.TrimSpace(c.Request.Form.Get("trade_no"))
	code := strings.TrimSpace(c.Request.Form.Get("qr_code"))
	if tradeNo == "" || code == "" {
		http.Error(c.Writer, "invalid checkout", http.StatusBadRequest)
		return
	}
	order := model.GetTopUpByTradeNo(tradeNo)
	if order == nil || order.PaymentProvider != model.PaymentProviderAlipay || order.Status != common.TopUpStatusPending || time.Now().Unix()-order.CreateTime > int64(alipayOrderTTL/time.Second) {
		http.Error(c.Writer, "payment order expired or unavailable", http.StatusGone)
		return
	}
	encoded, err := qr.Encode(code, qr.M, qr.Auto)
	if err != nil {
		http.Error(c.Writer, "QR unavailable", http.StatusInternalServerError)
		return
	}
	scaled, err := barcode.Scale(encoded, 256, 256)
	if err != nil {
		http.Error(c.Writer, "QR unavailable", http.StatusInternalServerError)
		return
	}
	var image bytes.Buffer
	if err := png.Encode(&image, scaled); err != nil {
		http.Error(c.Writer, "QR unavailable", http.StatusInternalServerError)
		return
	}
	returnURL := strings.TrimSpace(c.Request.Form.Get("return_url"))
	if returnURL == "" {
		returnURL = paymentReturnPath("/usage-logs")
	}
	page := template.Must(template.New("alipay-checkout").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>支付宝付款</title><style>body{font:18px sans-serif;max-width:420px;margin:3rem auto;text-align:center;padding:1rem}img{max-width:100%}</style><h1>支付宝扫码付款</h1><p>金额：¥{{.Amount}}</p><img alt="支付宝付款二维码" src="data:image/png;base64,{{.QR}}"><p>请核对金额，使用支付宝扫描二维码。支付完成后返回账户查看到账记录。</p><a href="{{.Return}}">返回账户</a></html>`))
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(c.Writer, struct{ Amount, QR, Return string }{Amount: fmt.Sprintf("%.2f", order.Money), QR: base64.StdEncoding.EncodeToString(image.Bytes()), Return: returnURL})
}

func AlipayNotify(c *gin.Context) {
	if !directAlipayConfigured() {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
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
	client, err := getDirectAlipayClient()
	if err != nil {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	notification, err := client.DecodeNotification(c.Request.Context(), values)
	if err != nil || notification == nil || notification.AppId != operation_setting.AlipayAppID || notification.OutTradeNo != order.TradeNo || notification.TradeNo == "" || (notification.TradeStatus != alipay.TradeStatusSuccess && notification.TradeStatus != alipay.TradeStatusFinished) {
		_, _ = io.WriteString(c.Writer, "fail")
		return
	}
	actual, err := decimal.NewFromString(notification.TotalAmount)
	expected := decimal.NewFromFloat(order.Money).Round(2)
	if err != nil || actual.Round(2).Cmp(expected) != 0 {
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

