package alipay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// The fixture verifies real merchant signatures and signs real SDK responses.
// No production credentials or live Alipay endpoints are used.
type gatewayFixture struct {
	client           *Client
	merchant         *rsa.PrivateKey
	alipay           *rsa.PrivateKey
	response         string
	corruptSignature bool
	requests         []url.Values
	config           Config
}

func newGatewayFixture(t *testing.T) *gatewayFixture {
	t.Helper()
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ali, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&ali.PublicKey)
	require.NoError(t, err)
	f := &gatewayFixture{merchant: merchant, alipay: ali, config: Config{
		AppID: "test-app", SellerID: "test-seller", Sandbox: true,
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(merchant)})),
		PublicKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
		NotifyURL:  "https://merchant.example/api/alipay/notify", ReturnURL: "https://merchant.example/topup",
	}}
	f.client, err = New(f.config)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.requests = append(f.requests, r.PostForm)
		signature, err := base64.StdEncoding.DecodeString(r.PostForm.Get("sign"))
		if err == nil {
			digest := sha256.Sum256([]byte(canonicalForm(r.PostForm, "sign")))
			err = rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], signature)
		}
		if err != nil {
			t.Errorf("merchant request signature rejected: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body := f.response
		digest := sha256.Sum256([]byte(body))
		signature, err = rsa.SignPKCS1v15(rand.Reader, ali, crypto.SHA256, digest[:])
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if f.corruptSignature {
			signature[0] ^= 1
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"%s_response":%s,"sign":"%s"}`,
			strings.ReplaceAll(r.PostForm.Get("method"), ".", "_"), body, base64.StdEncoding.EncodeToString(signature))
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	f.client.sdk.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Redirect only inside this test, preserving the real SDK's request path.
		req.URL.Scheme, req.URL.Host = endpoint.Scheme, endpoint.Host
		return http.DefaultTransport.RoundTrip(req)
	})
	return f
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func canonicalForm(values url.Values, ignored ...string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if !slices.Contains(ignored, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	return strings.Join(parts, "&")
}

func (f *gatewayFixture) notification(t *testing.T, changes map[string]string) string {
	t.Helper()
	values := url.Values{"app_id": {f.config.AppID}, "seller_id": {f.config.SellerID},
		"sign_type": {"RSA2"}, "out_trade_no": {"order-1"}, "trade_no": {"ali-1"},
		"trade_status": {"TRADE_SUCCESS"}, "total_amount": {"2.88"},
		"receipt_amount": {"2.00"}, "buyer_pay_amount": {"1.00"}}
	for key, value := range changes {
		values.Set(key, value)
	}
	digest := sha256.Sum256([]byte(canonicalForm(values, "sign", "sign_type", "alipay_cert_sn")))
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.alipay, crypto.SHA256, digest[:])
	require.NoError(t, err)
	values.Set("sign", base64.StdEncoding.EncodeToString(signature))
	return values.Encode()
}

func TestCNYAmountExact(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		cents int64
	}{
		{"0.01", 1}, {"2.88", 288}, {"1", 100}, {"1.2", 120}, {"100000000.00", MaxAmountCents},
	} {
		cents, err := ParseAmount(tc.raw)
		require.NoError(t, err, tc.raw)
		require.Equal(t, tc.cents, cents)
		formatted, err := FormatAmount(cents)
		require.NoError(t, err)
		roundTrip, err := ParseAmount(formatted)
		require.NoError(t, err)
		require.Equal(t, cents, roundTrip)
	}
	for _, raw := range []string{"", "0", "-1.00", "+1", "NaN", "Inf", "1e2", " 1.00", "1.001", "1.", ".01", "100000000.01", "999999999999999999999999999"} {
		_, err := ParseAmount(raw)
		require.Error(t, err, raw)
	}
}

func TestAlipayCheckoutRoutes(t *testing.T) {
	f := newGatewayFixture(t)
	ctx := context.Background()
	req := CreateRequest{Order: Order{OrderID: "order-1", AmountCents: 288}, Subject: "Test purchase"}
	f.response = `{"code":"10000","out_trade_no":"order-1","qr_code":"https://qr.alipay.com/test"}`
	checkout, err := f.client.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "https://qr.alipay.com/test", checkout.QRCode)
	require.Empty(t, checkout.PayURL)
	require.Len(t, f.requests, 1)
	require.Equal(t, "alipay.trade.precreate", f.requests[0].Get("method"))
	var biz map[string]string
	require.NoError(t, common.UnmarshalJsonStr(f.requests[0].Get("biz_content"), &biz))
	require.Equal(t, "FACE_TO_FACE_PAYMENT", biz["product_code"])
	require.Equal(t, "2.88", biz["total_amount"])
	require.Equal(t, f.config.NotifyURL, f.requests[0].Get("notify_url"))
	require.Equal(t, f.config.SellerID, biz["seller_id"])
	// Signed business failures and empty QR payloads fall back to a page URL.
	for _, response := range []string{`{"code":"40004","sub_code":"ACQ.ACCESS_FORBIDDEN"}`, `{"code":"10000","out_trade_no":"order-1","qr_code":""}`} {
		f.response = response
		checkout, err = f.client.Create(ctx, req)
		require.NoError(t, err)
		require.Empty(t, checkout.QRCode)
		payURL, err := url.Parse(checkout.PayURL)
		require.NoError(t, err)
		require.Equal(t, "alipay.trade.page.pay", payURL.Query().Get("method"))
	}
	req.IsMobile = true
	before := len(f.requests)
	checkout, err = f.client.Create(ctx, req)
	require.NoError(t, err)
	require.Len(t, f.requests, before)
	payURL, err := url.Parse(checkout.PayURL)
	require.NoError(t, err)
	require.Equal(t, "alipay.trade.wap.pay", payURL.Query().Get("method"))
	req.MobilePrecreate = true
	_, err = f.client.Create(ctx, req)
	require.Error(t, err, "mobile precreate must not silently return an unscannable page URL")
	f.response = `{"code":"10000","out_trade_no":"order-1","qr_code":"https://qr.alipay.com/test"}`
	checkout, err = f.client.Create(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, checkout.QRCode)
	f.client.config.PaymentMode = "redirect"
	req.IsMobile = false
	before = len(f.requests)
	checkout, err = f.client.Create(ctx, req)
	require.NoError(t, err)
	require.Empty(t, checkout.QRCode)
	require.NotEmpty(t, checkout.PayURL)
	require.Len(t, f.requests, before, "explicit redirect must skip the face-to-face request")
}

func TestAlipayNotificationBindsStoredOrder(t *testing.T) {
	f := newGatewayFixture(t)
	ctx := context.Background()
	order := Order{OrderID: "order-1", AmountCents: 288}
	payment, err := f.client.VerifyNotification(ctx, f.notification(t, nil), order)
	require.NoError(t, err)
	require.Equal(t, Paid, payment.State)
	require.EqualValues(t, 288, payment.AmountCents, "coupon-adjusted buyer payment is not the order total")
	for _, change := range []map[string]string{
		{"app_id": "another-app"}, {"seller_id": "another-seller"}, {"auth_app_id": "another-app"},
		{"out_trade_no": "another-order"}, {"total_amount": "2.87"}, {"total_amount": "NaN"},
		{"total_amount": ""}, {"trade_no": ""}, {"trade_status": "unknown"}, {"trans_currency": "USD"},
		{"sign_type": "RSA"}, {"alipay_cert_sn": "untrusted-cert"},
	} {
		_, err := f.client.VerifyNotification(ctx, f.notification(t, change), order)
		require.Error(t, err, change)
	}
	raw := f.notification(t, nil)
	_, err = f.client.VerifyNotification(ctx, raw+"&total_amount=2.88", order)
	require.Error(t, err, "parameter pollution must fail before decoding")
	_, err = f.client.VerifyNotification(ctx, strings.Replace(raw, "total_amount=2.88", "total_amount=2.89", 1), order)
	require.ErrorContains(t, err, "signature")
	for _, status := range []struct{ upstream, expected string }{{"WAIT_BUYER_PAY", Pending}, {"TRADE_CLOSED", Closed}, {"TRADE_FINISHED", Paid}} {
		payment, err := f.client.VerifyNotification(ctx, f.notification(t, map[string]string{"trade_status": status.upstream}), order)
		require.NoError(t, err)
		require.Equal(t, status.expected, payment.State)
	}
	require.Empty(t, f.requests, "untrusted certificate serials must not trigger network downloads")
}

func TestAlipayQueryAndCloseBusinessResults(t *testing.T) {
	f := newGatewayFixture(t)
	ctx := context.Background()
	order := Order{OrderID: "order-1", AmountCents: 288}
	f.response = `{"code":"10000","out_trade_no":"order-1","trade_no":"ali-1","total_amount":"2.88","trade_status":"TRADE_SUCCESS"}`
	payment, err := f.client.Query(ctx, order)
	require.NoError(t, err)
	require.Equal(t, Paid, payment.State)
	f.corruptSignature = true
	_, err = f.client.Query(ctx, order)
	require.Error(t, err)
	f.corruptSignature = false
	f.response = `{"code":"40004","sub_code":"ACQ.TRADE_NOT_EXIST"}`
	payment, err = f.client.Query(ctx, order)
	require.NoError(t, err)
	require.Equal(t, Pending, payment.State)
	require.NoError(t, f.client.Close(ctx, order.OrderID))
	f.response = `{"code":"40004","sub_code":"ACQ.SYSTEM_ERROR"}`
	_, err = f.client.Query(ctx, order)
	require.Error(t, err)
	require.Error(t, f.client.Close(ctx, order.OrderID), "signed business failure must not be treated as a successful close")
	f.response = `{"code":"10000","out_trade_no":"order-1","trade_no":"ali-1"}`
	require.NoError(t, f.client.Close(ctx, order.OrderID))
}

func TestAlipayRefundRetryAndReconciliation(t *testing.T) {
	f := newGatewayFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	refund := Refund{Order: Order{OrderID: "order-1", TradeNo: "ali-1", AmountCents: 288},
		RefundRequestID: "refund-1", RefundCents: 88, RequestedAt: now, Reason: "Test refund"}
	f.response = `{"code":"10000","out_trade_no":"order-1","trade_no":"ali-1","fund_change":"N"}`
	for range 2 {
		state, err := f.client.Refund(ctx, refund)
		require.NoError(t, err)
		require.Equal(t, Pending, state)
	}
	for _, request := range f.requests {
		var biz map[string]string
		require.NoError(t, common.UnmarshalJsonStr(request.Get("biz_content"), &biz))
		require.Equal(t, "refund-1", biz["out_request_no"])
		require.Equal(t, "0.88", biz["refund_amount"])
	}
	f.response = `{"code":"10000","out_trade_no":"order-1","trade_no":"ali-1","fund_change":"Y","refund_fee":"0.88"}`
	state, err := f.client.Refund(ctx, refund)
	require.NoError(t, err)
	require.Equal(t, Refunded, state)
	f.response = strings.Replace(f.response, `"0.88"`, `"0.89"`, 1)
	state, err = f.client.Refund(ctx, refund)
	require.Error(t, err)
	require.Equal(t, Pending, state)
	f.response = `{"code":"10000","out_trade_no":"order-1","trade_no":"ali-1","out_request_no":"refund-1","refund_amount":"0.88","refund_status":"REFUND_SUCCESS"}`
	state, err = f.client.QueryRefund(ctx, refund, now)
	require.NoError(t, err)
	require.Equal(t, Refunded, state)
	f.response = strings.Replace(f.response, `"0.88"`, `"0.89"`, 1)
	state, err = f.client.QueryRefund(ctx, refund, now)
	require.Error(t, err)
	require.Equal(t, Pending, state)
	f.response = `{"code":"10000"}`
	state, err = f.client.QueryRefund(ctx, refund, now.Add(9*time.Minute))
	require.NoError(t, err)
	require.Equal(t, Pending, state)
	state, err = f.client.QueryRefund(ctx, refund, now.Add(10*time.Minute))
	require.NoError(t, err)
	require.Equal(t, RefundFailed, state)
	f.response = `{"code":"40004","sub_code":"ACQ.SYSTEM_ERROR"}`
	state, err = f.client.QueryRefund(ctx, refund, now.Add(time.Hour))
	require.Error(t, err)
	require.Equal(t, Pending, state, "a signed system error is not proof of no refund")
	state, err = f.client.Refund(ctx, refund)
	require.Error(t, err)
	require.Equal(t, Pending, state)
	refund.RefundRequestID = ""
	before := len(f.requests)
	_, err = f.client.Refund(ctx, refund)
	require.Error(t, err)
	require.Len(t, f.requests, before, "missing persisted refund ID must fail without contacting Alipay")
}
