package controller

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type AlipayPayRequest struct {
	RequestID       string `json:"request_id"`
	Amount          int64  `json:"amount"`
	PlanID          int    `json:"plan_id"`
	IsMobile        bool   `json:"is_mobile"`
	MobilePrecreate bool   `json:"mobile_precreate"`
}

func RequestAlipayPay(c *gin.Context)             { requestAlipayPay(c, false) }
func SubscriptionRequestAlipayPay(c *gin.Context) { requestAlipayPay(c, true) }

func requestAlipayPay(c *gin.Context, subscription bool) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req AlipayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RequestID == "" || len(req.RequestID) > 64 ||
		(subscription && (req.PlanID <= 0 || req.Amount != 0)) || (!subscription && (req.PlanID != 0 || req.Amount <= 0)) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	client, settings, err := service.GetAlipayDirectClient()
	if err != nil {
		common.ApiErrorT(c, "Payment information has not been configured by administrator")
		return
	}
	userID := c.GetInt("id")
	order, err := model.GetAlipayOrderByRequest(userID, req.RequestID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiError(c, err)
		return
	}
	resuming := order != nil
	if order == nil {
		if !settings.Enabled {
			common.ApiErrorT(c, "Payment information has not been configured by administrator")
			return
		}
		order = &model.AlipayOrder{UserId: userID, RequestId: req.RequestID, OrderId: "NALI" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			AppId: settings.AppID, SellerId: settings.SellerID, PlanId: req.PlanID, RequestedAmount: req.Amount}
		var plan *model.SubscriptionPlan
		var walletReceiptAmount int64
		if subscription {
			plan, err = model.GetSubscriptionPlanById(req.PlanID)
			if err == nil && plan != nil && plan.Enabled {
				order.AmountCents, err = service.AlipayPlanAmount(decimal.NewFromFloat(plan.PriceAmount), plan.Currency, settings)
			} else {
				common.ApiErrorT(c, "Subscription plan is not enabled")
				return
			}
		} else {
			if req.Amount < getMinTopup() {
				common.ApiErrorT(c, "Payment amount is too low")
				return
			}
			order.CreditedQuota, err = validateTopUpQuota(req.Amount)
			if err == nil {
				walletReceiptAmount = req.Amount
				if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
					walletReceiptAmount = decimal.NewFromInt(req.Amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
				}
				var group string
				group, err = model.GetUserGroup(userID, true)
				if err == nil {
					order.AmountCents, err = pay.ParseAmount(decimal.NewFromFloat(getPayMoney(req.Amount, group)).Round(2).StringFixed(2))
				}
			}
		}
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if err := model.CreateAlipayOrder(order, plan, walletReceiptAmount); err != nil {
			// Another request can win the unique idempotency-key insert. Recover
			// its snapshot rather than creating a second external payment order.
			recovered, readErr := model.GetAlipayOrderByRequest(userID, req.RequestID)
			if readErr != nil {
				common.ApiError(c, err)
				return
			}
			order = recovered
			resuming = true
		}
	}
	if order.PlanId != req.PlanID || order.RequestedAmount != req.Amount || order.AppId != settings.AppID || order.SellerId != settings.SellerID {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if resuming && (order.Status == common.TopUpStatusPending || order.Status == model.AlipayOrderClosing) {
		order, err = service.ReconcileAlipayOrder(c.Request.Context(), order, c.ClientIP())
		if err != nil {
			common.ApiErrorT(c, "Failed to start payment")
			return
		}
	}
	if order.Status == common.TopUpStatusSuccess || order.Status == model.AlipayOrderClosed || order.Status == model.AlipayOrderClosing {
		common.ApiSuccess(c, gin.H{"order_id": order.OrderId, "status": order.Status, "amount_cents": order.AmountCents, "currency": "CNY"})
		return
	}
	if order.Status != common.TopUpStatusPending {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	subject := "Account top-up"
	if subscription {
		var plan model.SubscriptionPlan
		if err := common.UnmarshalJsonStr(order.PlanSnapshot, &plan); err != nil {
			common.ApiErrorT(c, "Failed to start payment")
			return
		}
		subject = plan.Title
	}
	checkout, err := client.Create(c.Request.Context(), pay.CreateRequest{Order: pay.Order{OrderID: order.OrderId, AmountCents: order.AmountCents},
		Subject: subject, IsMobile: req.IsMobile, MobilePrecreate: req.MobilePrecreate})
	if err != nil {
		// A timeout is not an unpaid/failed order. Keep the order pending so a
		// later query or signed callback can reconcile the same external number.
		common.ApiErrorT(c, "Failed to start payment")
		return
	}
	// A concurrent cancellation/payment may have completed while the provider
	// created checkout. Re-read before exposing any now-obsolete QR or page URL.
	current, err := model.GetAlipayOrder(order.OrderId)
	if err != nil {
		common.ApiErrorT(c, "Failed to start payment")
		return
	}
	if current.Status != common.TopUpStatusPending {
		common.ApiSuccess(c, gin.H{"order_id": current.OrderId, "status": current.Status, "amount_cents": current.AmountCents, "currency": "CNY"})
		return
	}
	common.ApiSuccess(c, gin.H{"order_id": order.OrderId, "status": order.Status, "amount_cents": order.AmountCents, "currency": "CNY",
		"qr_code": checkout.QRCode, "pay_url": checkout.PayURL})
}

// AlipayNotify accepts only the POST body, never a mixture of query parameters
// and form fields. The redirect/return URL has no fulfillment side effect.
func AlipayNotify(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusBadRequest, "failure")
		return
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values["out_trade_no"]) != 1 {
		c.String(http.StatusBadRequest, "failure")
		return
	}
	order, err := model.GetAlipayOrder(values.Get("out_trade_no"))
	if err != nil {
		c.String(http.StatusBadRequest, "failure")
		return
	}
	client, settings, err := service.GetAlipayDirectClient()
	if err != nil || order.AppId != settings.AppID || order.SellerId != settings.SellerID {
		c.String(http.StatusServiceUnavailable, "failure")
		return
	}
	expected := pay.Order{OrderID: order.OrderId, AmountCents: order.AmountCents}
	if order.TradeNo != nil {
		expected.TradeNo = *order.TradeNo
	}
	payment, err := client.VerifyNotification(c.Request.Context(), string(body), expected)
	if err != nil {
		c.String(http.StatusBadRequest, "failure")
		return
	}
	if payment.State == pay.Pending {
		c.String(http.StatusOK, "success")
		return
	}
	// Independently query the gateway before granting anything. A delayed or
	// reordered callback does not prove the current paid/refunded state.
	confirmed, err := client.Query(c.Request.Context(), expected)
	if err != nil || confirmed.TradeNo != payment.TradeNo {
		c.String(http.StatusServiceUnavailable, "failure")
		return
	}
	switch confirmed.State {
	case pay.Paid:
		_, err = model.CompleteAlipayOrder(*confirmed, settings.AppID, settings.SellerID, c.ClientIP())
	case pay.Closed:
		err = model.CompleteAlipayOrderClose(*confirmed, settings.AppID, settings.SellerID)
	default:
		err = errors.New("alipay notification requires reconciliation")
	}
	if err != nil {
		c.String(http.StatusServiceUnavailable, "failure")
		return
	}
	c.String(http.StatusOK, "success")
}

// QueryAlipayOrder is authenticated and ownership checked before querying any
// gateway data. It also reconciles a payment whose notification was lost.
func QueryAlipayOrder(c *gin.Context) {
	order, err := model.GetAlipayOrder(c.Param("order_id"))
	if err != nil || order.UserId != c.GetInt("id") {
		c.Status(http.StatusNotFound)
		return
	}
	if order.Status != common.TopUpStatusPending && order.Status != model.AlipayOrderClosing {
		common.ApiSuccess(c, order)
		return
	}
	order, err = service.ReconcileAlipayOrder(c.Request.Context(), order, c.ClientIP())
	if err != nil {
		common.ApiErrorT(c, "Failed to start payment")
		return
	}
	common.ApiSuccess(c, order)
}

// CancelAlipayOrder changes only an owned unpaid order. The closing marker is
// durable before the external request and remains visible on uncertain results.
func CancelAlipayOrder(c *gin.Context) {
	order, err := model.GetAlipayOrder(c.Param("order_id"))
	if err != nil || order.UserId != c.GetInt("id") {
		c.Status(http.StatusNotFound)
		return
	}
	order, err = model.BeginAlipayOrderClose(order.OrderId, c.GetInt("id"))
	if err != nil {
		common.ApiErrorT(c, "Payment status could not be verified.")
		return
	}
	resolved, err := service.ReconcileAlipayOrder(c.Request.Context(), order, c.ClientIP())
	if err == nil {
		order = resolved
	}
	// status=closing means cancellation was requested but not confirmed. It is
	// deliberately distinct from both a closed order and a refunded payment.
	common.ApiSuccess(c, order)
}

func isAlipayTopUpEnabled() bool {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return false
	}
	settings, err := service.LoadAlipayDirectSettings()
	return err == nil && settings.Enabled
}

func ListAlipayOrders(c *gin.Context) {
	orders, err := model.ListRecentAlipayOrders(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, orders)
}
