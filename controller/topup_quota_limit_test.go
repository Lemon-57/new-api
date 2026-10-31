package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Exercise native HTTP handlers with real SDK signature verification. Only the
// SDK's HTTP transport is routed to an in-process signed test gateway.
func TestAlipayDirectCheckoutAndNotification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldPath, oldMaster, oldRedis := common.SQLitePath, common.IsMasterNode, common.RedisEnabled
	oldScale, oldPrice := common.QuotaPerUnit, operation_setting.Price
	oldMin, oldDisplay := operation_setting.MinTopUp, operation_setting.GetGeneralSetting().QuotaDisplayType
	compliance := operation_setting.GetPaymentSetting()
	oldCompliance := *compliance
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	common.SQLitePath = filepath.Join(t.TempDir(), "alipay-http.db") + "?_txlock=immediate&_pragma=busy_timeout(30000)"
	common.IsMasterNode, common.RedisEnabled = true, false
	common.QuotaPerUnit, operation_setting.Price, operation_setting.MinTopUp = 500000, 2.88, 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	db, err := model.DB.DB()
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SQLitePath, common.IsMasterNode, common.RedisEnabled = oldPath, oldMaster, oldRedis
		common.QuotaPerUnit, operation_setting.Price, operation_setting.MinTopUp = oldScale, oldPrice, oldMin
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		*compliance = oldCompliance
	})
	user := model.User{Username: "alipay-http", Quota: 9, Group: "default", AffCode: "alipay-http", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ali, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&ali.PublicKey)
	require.NoError(t, err)
	settings := service.AlipayDirectSettings{Enabled: true, Config: pay.Config{AppID: "test-app", SellerID: "test-seller", Sandbox: true,
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(merchant)})),
		PublicKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})),
		NotifyURL:  "https://merchant.example/api/user/alipay/notify"}}
	config, err := common.Marshal(settings)
	require.NoError(t, err)
	configFile := filepath.Join(t.TempDir(), "alipay.private.json")
	require.NoError(t, os.WriteFile(configFile, config, 0600))
	t.Setenv("ALIPAY_DIRECT_CONFIG_FILE", configFile)
	var gatewayRequests int
	gatewayState := "WAIT_BUYER_PAY"
	oldTransport := http.DefaultTransport
	http.DefaultTransport = alipayHTTPTestTransport(func(req *http.Request) (*http.Response, error) {
		gatewayRequests++
		require.NoError(t, req.ParseForm())
		var biz map[string]any
		require.NoError(t, common.UnmarshalJsonStr(req.PostForm.Get("biz_content"), &biz))
		method := req.PostForm.Get("method")
		body := fmt.Sprintf(`{"code":"10000","out_trade_no":%q,"qr_code":"https://qr.alipay.com/test"}`, biz["out_trade_no"])
		if method == "alipay.trade.query" {
			body = fmt.Sprintf(`{"code":"10000","out_trade_no":%q,"trade_no":%q,"total_amount":"2.88","trade_status":%q}`, biz["out_trade_no"], "ali-"+biz["out_trade_no"].(string), gatewayState)
		} else {
			require.Equal(t, "alipay.trade.precreate", method)
		}
		digest := sha256.Sum256([]byte(body))
		signature, err := rsa.SignPKCS1v15(rand.Reader, ali, crypto.SHA256, digest[:])
		require.NoError(t, err)
		envelope := fmt.Sprintf(`{"%s_response":%s,"sign":"%s"}`, strings.ReplaceAll(method, ".", "_"), body, base64.StdEncoding.EncodeToString(signature))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(envelope))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id); c.Next() })
	router.POST("/pay", RequestAlipayPay)
	router.POST("/subscription", SubscriptionRequestAlipayPay)
	router.POST("/notify", AlipayNotify)
	router.GET("/orders/:order_id", QueryAlipayOrder)
	post := func(path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		return recorder
	}
	compliance.ComplianceConfirmed = false
	response := post("/pay", `{"request_id":"http-request","amount":1}`)
	require.Contains(t, response.Body.String(), "administrator must confirm")
	require.Zero(t, gatewayRequests)
	// Synthetic test state only; no real instance's declaration is changed.
	compliance.ComplianceConfirmed, compliance.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
	response = post("/pay", `{"request_id":"http-request","amount":1}`)
	require.Contains(t, response.Body.String(), "https://qr.alipay.com/test")
	order, err := model.GetAlipayOrderByRequest(user.Id, "http-request")
	require.NoError(t, err)
	require.EqualValues(t, 288, order.AmountCents)
	response = post("/pay", `{"request_id":"http-request","amount":1}`)
	require.Contains(t, response.Body.String(), order.OrderId)
	response = post("/pay", `{"request_id":"http-request","amount":2}`)
	require.Contains(t, response.Body.String(), "Invalid parameters")
	var count int64
	require.NoError(t, model.DB.Model(&model.AlipayOrder{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	values := url.Values{"app_id": {settings.AppID}, "seller_id": {settings.SellerID}, "sign_type": {"RSA2"},
		"out_trade_no": {order.OrderId}, "trade_no": {"ali-" + order.OrderId}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"2.88"}}
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "sign_type" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	signature, err := rsa.SignPKCS1v15(rand.Reader, ali, crypto.SHA256, digest[:])
	require.NoError(t, err)
	values.Set("sign", base64.StdEncoding.EncodeToString(signature))
	response = post("/notify", values.Encode())
	require.Equal(t, http.StatusServiceUnavailable, response.Code, "a valid paid callback alone cannot override a pending gateway query")
	var unchanged model.User
	require.NoError(t, model.DB.First(&unchanged, user.Id).Error)
	require.Equal(t, 9, unchanged.Quota)
	gatewayState = "TRADE_SUCCESS"
	for range 2 {
		response = post("/notify", values.Encode())
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, "success", response.Body.String())
	}
	require.NoError(t, model.DB.First(&unchanged, user.Id).Error)
	require.Equal(t, 500009, unchanged.Quota)
	response = post("/notify", values.Encode()+"&total_amount=2.88")
	require.Equal(t, http.StatusBadRequest, response.Code)
	before := gatewayRequests
	response = post("/pay", `{"request_id":"http-request","amount":1}`)
	require.Contains(t, response.Body.String(), common.TopUpStatusSuccess)
	require.Equal(t, before, gatewayRequests, "lost reply must return the committed order without another checkout")
	// Native receipts store currency units even when checkout accepts tokens.
	// The original request is retained separately for idempotency matching.
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	response = post("/pay", `{"request_id":"token-units","amount":500000}`)
	require.Contains(t, response.Body.String(), "https://qr.alipay.com/test")
	tokenOrder, err := model.GetAlipayOrderByRequest(user.Id, "token-units")
	require.NoError(t, err)
	require.EqualValues(t, 500000, tokenOrder.RequestedAmount)
	require.Equal(t, 500000, tokenOrder.CreditedQuota)
	require.EqualValues(t, 1, model.GetTopUpByTradeNo(tokenOrder.OrderId).Amount)
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	// A plan whose notification is lost is fulfilled by authenticated polling.
	plan := model.SubscriptionPlan{Title: "HTTP plan", PriceAmount: 2.88, Currency: "CNY", Enabled: true,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1600, UpgradeGroup: "http-pro"}
	require.NoError(t, model.DB.Create(&plan).Error)
	response = post("/subscription", fmt.Sprintf(`{"request_id":"plan-http","plan_id":%d}`, plan.Id))
	require.Contains(t, response.Body.String(), "https://qr.alipay.com/test")
	planOrder, err := model.GetAlipayOrderByRequest(user.Id, "plan-http")
	require.NoError(t, err)
	for range 2 {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/orders/"+planOrder.OrderId, nil))
		require.Contains(t, recorder.Body.String(), common.TopUpStatusSuccess)
	}
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, model.DB.First(&unchanged, user.Id).Error)
	require.Equal(t, "http-pro", unchanged.Group)
	require.Equal(t, 500009, unchanged.Quota, "subscription purchase must not also top up the wallet")
	before = gatewayRequests
	// Cross-user lookup must be rejected before contacting Alipay.
	user.Id++
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/orders/"+order.OrderId, nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, before, gatewayRequests)
}

type alipayHTTPTestTransport func(*http.Request) (*http.Response, error)

func (f alipayHTTPTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAlipayPlanCurrencyAmount(t *testing.T) {
	for _, tc := range []struct {
		name, price, currency, rate string
		cents                       int64
		invalid                     bool
	}{
		{name: "CNY keeps the plan price", price: "2.88", currency: "CNY", cents: 288},
		{name: "USD uses an explicit rate", price: "0.40", currency: "USD", rate: "7.2", cents: 288},
		{name: "CNY rounds at the gateway currency precision", price: "1.005", currency: "CNY", cents: 101},
		{name: "USD without a rate fails", price: "1", currency: "USD", invalid: true},
		{name: "negative rate fails", price: "1", currency: "USD", rate: "-7.2", invalid: true},
		{name: "unknown currency fails", price: "1", currency: "EUR", rate: "7.2", invalid: true},
		{name: "sub-cent total fails", price: "0.001", currency: "CNY", invalid: true},
		{name: "negative price fails", price: "-1", currency: "CNY", invalid: true},
		{name: "oversized total fails", price: "100000000.01", currency: "CNY", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price, err := decimal.NewFromString(tc.price)
			require.NoError(t, err)
			cents, err := service.AlipayPlanAmount(price, tc.currency, &service.AlipayDirectSettings{CNYPerUSD: tc.rate})
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.cents, cents)
		})
	}
}

func TestTopUpQuotaValidation(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	testCases := []struct {
		name        string
		displayType string
		amount      int64
		wantQuota   int
		wantErr     bool
	}{
		{
			name:        "currency amount below limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4294,
			wantQuota:   2_147_000_000,
		},
		{
			name:        "currency amount above limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4295,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount preserves settlement truncation",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      2_147_500_000,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount above legacy int32 range",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      4_294_500_000,
			wantQuota:   4_294_500_000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.displayType
			quota, err := getTopUpQuota(tc.amount)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantQuota, quota)
		})
	}
}

func TestValidateTopUpQuotaReturnsMaximumAmount(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()

	_, err := validateTopUpQuota(maxAmount)
	require.NoError(t, err)
	_, err = validateTopUpQuota(maxAmount + 1)
	require.EqualError(t, err, fmt.Sprintf("Top-up amount cannot exceed %d", maxAmount))
}

func TestRequestAmountRejectsTopUpThatCannotBeSettled(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(fmt.Sprintf(`{"amount":%d}`, maxAmount+1)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	RequestAmount(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, fmt.Sprintf(`{"message":"error","data":"Top-up amount cannot exceed %d","message_key":"Top-up amount cannot exceed {{max}}","message_params":{"max":%d}}`, maxAmount, maxAmount), recorder.Body.String())
}

func TestRequestAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldDB := model.DB
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       42,
		Username: "topup_capacity_user",
		Quota:    common.MaxWalletQuota - 100_000,
		Status:   common.UserStatusEnabled,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(`{"amount":1}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	RequestAmount(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`, recorder.Body.String())
}

func TestValidateCreditedQuotaRejectsOverflow(t *testing.T) {
	_, err := validateCreditedQuota(decimal.NewFromInt(int64(common.MaxWalletQuota / 2)))
	require.NoError(t, err)
	_, err = validateCreditedQuota(decimal.Zero)
	require.EqualError(t, err, "Top-up quota must be greater than 0")
	_, err = validateCreditedQuota(decimal.NewFromInt(common.MaxWalletQuota + 1))
	require.EqualError(
		t,
		err,
		"Top-up quota exceeds the range the system can represent",
	)
}

func TestStripeCreditedQuotaIncludesGroupRatio(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldTopupGroupRatio := common.TopupGroupRatio2JSONString()
	common.QuotaPerUnit = 500000
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"vip":2}`))
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldTopupGroupRatio))
	})

	_, err := validateCreditedQuota(getStripeCreditedQuota(2147, "vip"))
	require.NoError(t, err)
	_, err = validateCreditedQuota(getStripeCreditedQuota(2148, "vip"))
	require.NoError(t, err)
	_, err = validateCreditedQuota(getStripeCreditedQuota(int64(common.MaxWalletQuota), "vip"))
	require.Error(t, err)

	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"free":0}`))
	assert.True(t, decimal.NewFromInt(500000).Equal(getStripeCreditedQuota(1, "free")))
}
