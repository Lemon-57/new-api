// Package alipay adapts the face-to-face payment flow used by Sub2API
// (backend/internal/payment/provider/alipay.go) to New API. The SDK handles
// RSA2 signing and verification; callers own durable orders and fulfillment.
package alipay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	sdk "github.com/smartwalle/alipay/v3"
)

const (
	Pending                   = "pending"
	Paid                      = "paid"
	Closed                    = "closed"
	Refunded                  = "refunded"
	RefundFailed              = "failed"
	MaxAmountCents      int64 = 10000000000 // Alipay's 100,000,000 CNY limit.
	refundNotFoundGrace       = 10 * time.Minute
)

var (
	orderPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	amountPattern = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,2})?$`)
)

// Config accepts the same key names as Sub2API's existing Alipay instance.
// Credentials must never be included in a checkout response or diagnostic log.
type Config struct {
	AppID           string `json:"appId"`
	PrivateKey      string `json:"privateKey"`
	PublicKey       string `json:"publicKey"`
	AlipayPublicKey string `json:"alipayPublicKey"`
	SellerID        string `json:"sellerId"`
	NotifyURL       string `json:"notifyUrl"`
	ReturnURL       string `json:"returnUrl"`
	PaymentMode     string `json:"paymentMode"`
	Sandbox         bool   `json:"sandbox"`
}

type Client struct {
	config Config
	sdk    *sdk.Client
}

// Settings preserves Sub2API's credential names for both the private-file and
// managed configuration paths. Callers must never return this type to clients.
type Settings struct {
	Config
	Enabled   bool   `json:"enabled"`
	CNYPerUSD string `json:"cnyPerUsd"`
}

func New(config Config) (*Client, error) {
	if strings.TrimSpace(config.AppID) == "" || config.PrivateKey == "" {
		return nil, errors.New("alipay appId and privateKey are required")
	}
	publicKey := config.PublicKey
	if publicKey == "" {
		publicKey = config.AlipayPublicKey
	}
	if publicKey == "" {
		return nil, errors.New("alipay publicKey is required")
	}
	client, err := sdk.New(config.AppID, config.PrivateKey, !config.Sandbox,
		sdk.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}))
	if err != nil {
		return nil, errors.New("invalid alipay application private key")
	}
	if err := client.LoadAliPayPublicKey(publicKey); err != nil {
		return nil, errors.New("invalid alipay public key")
	}
	return &Client{config: config, sdk: client}, nil
}

// ParseAmount parses CNY without floating-point rounding or an alternate amount
// fallback. receipt_amount/buyer_pay_amount may differ due to discounts and must
// never substitute for the order's total_amount.
func ParseAmount(raw string) (int64, error) {
	if !amountPattern.MatchString(raw) {
		return 0, errors.New("invalid alipay CNY amount")
	}
	whole, fraction, _ := strings.Cut(raw, ".")
	yuan, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, errors.New("invalid alipay CNY amount")
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	fen, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, errors.New("invalid alipay CNY amount")
	}
	cents := yuan*100 + fen
	if cents <= 0 || cents > MaxAmountCents {
		return 0, errors.New("alipay CNY amount is out of range")
	}
	return cents, nil
}

func FormatAmount(cents int64) (string, error) {
	if cents <= 0 || cents > MaxAmountCents {
		return "", errors.New("alipay CNY amount is out of range")
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100), nil
}

// Order is an immutable server-side snapshot, never populated from a callback.
type Order struct {
	OrderID     string
	AmountCents int64
	TradeNo     string
}

func (o Order) validate() error {
	if !orderPattern.MatchString(o.OrderID) {
		return errors.New("invalid alipay merchant order number")
	}
	_, err := FormatAmount(o.AmountCents)
	return err
}

type CreateRequest struct {
	Order
	Subject         string
	IsMobile        bool
	MobilePrecreate bool
}

type Checkout struct {
	OrderID string `json:"order_id"`
	QRCode  string `json:"qr_code,omitempty"`
	PayURL  string `json:"pay_url,omitempty"`
}

func (c *Client) Create(ctx context.Context, req CreateRequest) (*Checkout, error) {
	if err := req.Order.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Subject) == "" || len([]rune(req.Subject)) > 256 {
		return nil, errors.New("invalid alipay order subject")
	}
	amount, _ := FormatAmount(req.AmountCents)
	trade := sdk.Trade{OutTradeNo: req.OrderID, TotalAmount: amount, Subject: req.Subject,
		NotifyURL: c.config.NotifyURL, ReturnURL: c.config.ReturnURL, SellerId: c.config.SellerID}
	if req.IsMobile && !req.MobilePrecreate {
		trade.ProductCode = "QUICK_WAP_WAY"
		payURL, err := c.sdk.TradeWapPay(sdk.TradeWapPay{Trade: trade})
		if err != nil {
			return nil, errors.New("alipay mobile checkout creation failed")
		}
		return &Checkout{OrderID: req.OrderID, PayURL: payURL.String()}, nil
	}
	if req.IsMobile || !strings.EqualFold(strings.TrimSpace(c.config.PaymentMode), "redirect") {
		trade.ProductCode = "FACE_TO_FACE_PAYMENT"
		result, err := c.sdk.TradePreCreate(ctx, sdk.TradePreCreate{Trade: trade})
		if err == nil && result != nil && !result.IsFailure() && strings.TrimSpace(result.QRCode) != "" && result.OutTradeNo == req.OrderID {
			return &Checkout{OrderID: req.OrderID, QRCode: result.QRCode}, nil
		}
		if req.IsMobile || ctx.Err() != nil {
			return nil, errors.New("alipay face-to-face checkout creation failed")
		}
	}
	// Like Sub2API, merchants without face-to-face access can use page pay.
	// A checkout-page URL is never returned as QRCode.
	trade.ProductCode = "FAST_INSTANT_TRADE_PAY"
	payURL, err := c.sdk.TradePagePay(sdk.TradePagePay{Trade: trade})
	if err != nil {
		return nil, errors.New("alipay desktop checkout creation failed")
	}
	return &Checkout{OrderID: req.OrderID, PayURL: payURL.String()}, nil
}

type Payment struct {
	OrderID     string
	TradeNo     string
	AmountCents int64
	State       string
}

func paymentState(status sdk.TradeStatus) (string, error) {
	switch status {
	case sdk.TradeStatusSuccess, sdk.TradeStatusFinished:
		return Paid, nil
	case sdk.TradeStatusClosed:
		return Closed, nil
	case "WAIT_BUYER_PAY":
		return Pending, nil
	default:
		return "", errors.New("unknown alipay trade status")
	}
}

func matchPayment(order Order, orderID, tradeNo, amount string, status sdk.TradeStatus) (*Payment, error) {
	if order.OrderID != orderID {
		return nil, errors.New("alipay order number mismatch")
	}
	cents, err := ParseAmount(amount)
	if err != nil || cents != order.AmountCents {
		return nil, errors.New("alipay order total amount mismatch")
	}
	state, err := paymentState(status)
	if err != nil {
		return nil, err
	}
	if (state == Paid && tradeNo == "") || (order.TradeNo != "" && order.TradeNo != tradeNo) {
		return nil, errors.New("alipay transaction number mismatch")
	}
	return &Payment{OrderID: orderID, TradeNo: tradeNo, AmountCents: cents, State: state}, nil
}

func (c *Client) Query(ctx context.Context, order Order) (*Payment, error) {
	if err := order.validate(); err != nil {
		return nil, err
	}
	result, err := c.sdk.TradeQuery(ctx, sdk.TradeQuery{OutTradeNo: order.OrderID})
	if err != nil {
		var businessError *sdk.Error
		if errors.As(err, &businessError) && businessError.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return &Payment{OrderID: order.OrderID, State: Pending}, nil
		}
		return nil, errors.New("alipay order query failed")
	}
	if result == nil {
		return nil, errors.New("empty alipay order query response")
	}
	if result.IsFailure() {
		if result.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return &Payment{OrderID: order.OrderID, State: Pending}, nil
		}
		return nil, errors.New("alipay order query rejected")
	}
	if result.TransCurrency != "" && result.TransCurrency != "CNY" {
		return nil, errors.New("alipay order currency mismatch")
	}
	return matchPayment(order, result.OutTradeNo, result.TradeNo, result.TotalAmount, result.TradeStatus)
}

// VerifyNotification checks the signature AND the stored merchant/order facts.
// The HTTP handler must acknowledge only after durable, idempotent fulfillment.
func (c *Client) VerifyNotification(ctx context.Context, raw string, order Order) (*Payment, error) {
	if err := order.validate(); err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, errors.New("invalid alipay notification form")
	}
	for _, entries := range values {
		if len(entries) != 1 {
			return nil, errors.New("duplicate alipay notification field")
		}
	}
	// A public-key integration must not let an untrusted certificate serial
	// trigger the SDK's automatic certificate download before authentication.
	if values.Get("sign_type") != "RSA2" || values.Get("alipay_cert_sn") != "" {
		return nil, errors.New("unsupported alipay notification signature mode")
	}
	notification, err := c.sdk.DecodeNotification(ctx, values)
	if err != nil || notification == nil {
		return nil, errors.New("invalid alipay notification signature")
	}
	if notification.AppId != c.config.AppID || (notification.AuthAppId != "" && notification.AuthAppId != c.config.AppID) {
		return nil, errors.New("alipay notification application mismatch")
	}
	if c.config.SellerID != "" && notification.SellerId != c.config.SellerID {
		return nil, errors.New("alipay notification seller mismatch")
	}
	if currency := values.Get("trans_currency"); currency != "" && currency != "CNY" {
		return nil, errors.New("alipay notification currency mismatch")
	}
	return matchPayment(order, notification.OutTradeNo, notification.TradeNo, notification.TotalAmount, notification.TradeStatus)
}

// RefundRequestID must be persisted BEFORE calling Refund, then reused after
// timeouts or process restarts. Generating a new ID on retry can refund twice.
type Refund struct {
	Order
	RefundRequestID string
	RefundCents     int64
	Reason          string
	RequestedAt     time.Time
}

func (r Refund) validate() error {
	if err := r.Order.validate(); err != nil {
		return err
	}
	if !orderPattern.MatchString(r.RefundRequestID) || r.RefundCents <= 0 || r.RefundCents > r.AmountCents {
		return errors.New("invalid alipay refund request")
	}
	return nil
}

func (c *Client) Refund(ctx context.Context, refund Refund) (string, error) {
	if err := refund.validate(); err != nil {
		return "", err
	}
	amount, _ := FormatAmount(refund.RefundCents)
	result, err := c.sdk.TradeRefund(ctx, sdk.TradeRefund{OutTradeNo: refund.OrderID,
		OutRequestNo: refund.RefundRequestID, RefundAmount: amount, RefundReason: refund.Reason})
	if err != nil || result == nil {
		return Pending, errors.New("alipay refund result is uncertain; query the persisted refund request")
	}
	if result.IsFailure() {
		return Pending, errors.New("alipay refund rejected; reconcile before retrying")
	}
	if result.OutTradeNo != refund.OrderID || (refund.TradeNo != "" && result.TradeNo != refund.TradeNo) {
		return Pending, errors.New("alipay refund order mismatch")
	}
	if result.FundChange == "Y" {
		cents, err := ParseAmount(result.RefundFee)
		if err != nil || cents != refund.RefundCents {
			return Pending, errors.New("alipay refund amount mismatch")
		}
		return Refunded, nil
	}
	return Pending, nil
}

func (c *Client) QueryRefund(ctx context.Context, refund Refund, now time.Time) (string, error) {
	if err := refund.validate(); err != nil {
		return "", err
	}
	result, err := c.sdk.TradeFastPayRefundQuery(ctx, sdk.TradeFastPayRefundQuery{
		OutTradeNo: refund.OrderID, OutRequestNo: refund.RefundRequestID})
	if err != nil || result == nil || result.IsFailure() {
		return Pending, errors.New("alipay refund query failed")
	}
	if result.RefundStatus == "REFUND_SUCCESS" {
		cents, err := ParseAmount(result.RefundAmount)
		if err != nil || cents != refund.RefundCents || result.OutRequestNo != refund.RefundRequestID ||
			result.OutTradeNo != refund.OrderID || (refund.TradeNo != "" && result.TradeNo != refund.TradeNo) {
			return Pending, errors.New("alipay refund query identity or amount mismatch")
		}
		return Refunded, nil
	}
	if result.OutRequestNo == "" && result.RefundAmount == "" && !refund.RequestedAt.IsZero() && now.Sub(refund.RequestedAt) >= refundNotFoundGrace {
		return RefundFailed, nil
	}
	return Pending, nil
}

func (c *Client) Close(ctx context.Context, orderID string) error {
	if !orderPattern.MatchString(orderID) {
		return errors.New("invalid alipay merchant order number")
	}
	result, err := c.sdk.TradeClose(ctx, sdk.TradeClose{OutTradeNo: orderID})
	if err != nil {
		var businessError *sdk.Error
		if errors.As(err, &businessError) && businessError.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return nil
		}
		return errors.New("alipay close result is uncertain; query the order")
	}
	if result == nil {
		return errors.New("empty alipay close response")
	}
	if result.IsFailure() {
		if result.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return nil
		}
		return errors.New("alipay order close rejected")
	}
	if result.OutTradeNo != orderID {
		return errors.New("alipay close order mismatch")
	}
	return nil
}
