package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func TestStripeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	setting.StripeWebhookSecret = ""
	setting.StripeApiSecret = "sk_test_123"
	setting.StripePriceId = "price_123"
	require.False(t, isStripeWebhookEnabled())

	setting.StripeWebhookSecret = "whsec_test"
	require.True(t, isStripeWebhookEnabled())

	setting.StripePriceId = ""
	require.False(t, isStripeWebhookEnabled())
}

func TestCreemWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPIKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalWebhookSecret := setting.CreemWebhookSecret
	t.Cleanup(func() {
		setting.CreemApiKey = originalAPIKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalWebhookSecret
	})

	setting.CreemWebhookSecret = ""
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = `[{"productId":"prod_123"}]`
	require.False(t, isCreemWebhookEnabled())

	setting.CreemWebhookSecret = "creem_secret"
	require.True(t, isCreemWebhookEnabled())

	setting.CreemProducts = "[]"
	require.False(t, isCreemWebhookEnabled())
}

func TestWaffoWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalEnabled := setting.WaffoEnabled
	originalSandbox := setting.WaffoSandbox
	originalAPIKey := setting.WaffoApiKey
	originalPrivateKey := setting.WaffoPrivateKey
	originalPublicCert := setting.WaffoPublicCert
	originalSandboxAPIKey := setting.WaffoSandboxApiKey
	originalSandboxPrivateKey := setting.WaffoSandboxPrivateKey
	originalSandboxPublicCert := setting.WaffoSandboxPublicCert
	t.Cleanup(func() {
		setting.WaffoEnabled = originalEnabled
		setting.WaffoSandbox = originalSandbox
		setting.WaffoApiKey = originalAPIKey
		setting.WaffoPrivateKey = originalPrivateKey
		setting.WaffoPublicCert = originalPublicCert
		setting.WaffoSandboxApiKey = originalSandboxAPIKey
		setting.WaffoSandboxPrivateKey = originalSandboxPrivateKey
		setting.WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = ""
	setting.WaffoPrivateKey = "private"
	setting.WaffoPublicCert = "public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoApiKey = "api"
	require.True(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = false
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = true
	setting.WaffoSandbox = true
	setting.WaffoSandboxApiKey = ""
	setting.WaffoSandboxPrivateKey = "sandbox_private"
	setting.WaffoSandboxPublicCert = "sandbox_public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoSandboxApiKey = "sandbox_api"
	require.True(t, isWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalMerchantID := setting.WaffoPancakeMerchantID
	originalPrivateKey := setting.WaffoPancakePrivateKey
	originalProductID := setting.WaffoPancakeProductID
	t.Cleanup(func() {
		setting.WaffoPancakeMerchantID = originalMerchantID
		setting.WaffoPancakePrivateKey = originalPrivateKey
		setting.WaffoPancakeProductID = originalProductID
	})

	// Presence of all three credentials enables the gateway. Webhook public
	// keys are bundled in the SDK and there is no separate Enabled toggle —
	// clear any of the three fields to disable.
	setting.WaffoPancakeMerchantID = ""
	setting.WaffoPancakePrivateKey = "private"
	setting.WaffoPancakeProductID = "product"
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeMerchantID = "merchant"
	require.True(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeProductID = ""
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeProductID = "product"
	setting.WaffoPancakePrivateKey = ""
	require.False(t, isWaffoPancakeWebhookEnabled())
}

func TestEpayWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	paymentSetting := operation_setting.GetPaymentSetting()
	originalSignType := paymentSetting.EpaySignType
	originalMerchantPrivateKey := paymentSetting.EpayMerchantPrivateKey
	originalPlatformPublicKey := paymentSetting.EpayPlatformPublicKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		paymentSetting.EpaySignType = originalSignType
		paymentSetting.EpayMerchantPrivateKey = originalMerchantPrivateKey
		paymentSetting.EpayPlatformPublicKey = originalPlatformPublicKey
		operation_setting.PayMethods = originalPayMethods
	})

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = ""
	paymentSetting.EpaySignType = operation_setting.EpaySignTypeMD5
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	require.False(t, isEpayWebhookEnabled())

	operation_setting.EpayKey = "epay_key"
	require.True(t, isEpayWebhookEnabled())

	operation_setting.PayMethods = nil
	require.False(t, isEpayWebhookEnabled())

	// RSA needs both keys, and the MD5 key no longer counts.
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	paymentSetting.EpaySignType = operation_setting.EpaySignTypeRSA
	paymentSetting.EpayMerchantPrivateKey = "merchant_private_key"
	paymentSetting.EpayPlatformPublicKey = ""
	require.False(t, isEpayWebhookEnabled())

	paymentSetting.EpayPlatformPublicKey = "platform_public_key"
	operation_setting.EpayKey = ""
	require.True(t, isEpayWebhookEnabled())
}

type alipayTestTransport func(*http.Request) (*http.Response, error)

func (transport alipayTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestDirectAlipayWalletPayment(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	oldOptions, oldMethods, oldClient := common.OptionMap, operation_setting.PayMethods, http.DefaultClient
	oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	oldQuota, oldPrice, oldMinimum := common.QuotaPerUnit, operation_setting.Price, operation_setting.MinTopUp
	general := operation_setting.GetGeneralSetting()
	oldDisplay := general.QuotaDisplayType
	t.Cleanup(func() {
		common.OptionMap, operation_setting.PayMethods, http.DefaultClient = oldOptions, oldMethods, oldClient
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
		common.QuotaPerUnit, operation_setting.Price, operation_setting.MinTopUp = oldQuota, oldPrice, oldMinimum
		general.QuotaDisplayType = oldDisplay
	})
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "", "", ""
	common.QuotaPerUnit, operation_setting.Price, operation_setting.MinTopUp = 500000, 1, 1
	general.QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}, {"type": "wxpay"}}
	merchantKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	providerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	providerPublic, err := x509.MarshalPKIXPublicKey(&providerKey.PublicKey)
	require.NoError(t, err)
	rotatedProvider, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rotatedPublic, err := x509.MarshalPKIXPublicKey(&rotatedProvider.PublicKey)
	require.NoError(t, err)
	options := map[string]string{
		"AlipayAppID":      "synthetic-merchant",
		"AlipayPrivateKey": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(merchantKey)),
		"AlipayPublicKey":  base64.StdEncoding.EncodeToString(providerPublic),
		"AlipayNotifyURL":  "https://merchant.example/api/user/alipay/notify",
		"AlipayReturnURL":  "https://merchant.example/wallet",
	}
	sign := func(value string, key *rsa.PrivateKey) string {
		digest := sha256.Sum256([]byte(value))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		return base64.StdEncoding.EncodeToString(signature)
	}
	precreates := 0
	http.DefaultClient = &http.Client{Transport: alipayTestTransport(func(request *http.Request) (*http.Response, error) {
		precreates++
		require.Equal(t, "openapi.alipay.com", request.URL.Hostname())
		require.NoError(t, request.ParseForm())
		require.Equal(t, "alipay.trade.precreate", request.Form.Get("method"))
		require.Equal(t, options["AlipayNotifyURL"], request.Form.Get("notify_url"))
		var business map[string]string
		require.NoError(t, common.UnmarshalJsonStr(request.Form.Get("biz_content"), &business))
		require.Equal(t, "10.00", business["total_amount"])
		require.Equal(t, "FACE_TO_FACE_PAYMENT", business["product_code"])
		body := fmt.Sprintf(`{"code":"10000","out_trade_no":%q,"qr_code":"https://qr.alipay.com/synthetic-wallet"}`, business["out_trade_no"])
		key := providerKey
		if request.Form.Get("app_id") == "synthetic-rotated" {
			key = rotatedProvider
		}
		response := fmt.Sprintf(`{"alipay_trade_precreate_response":%s,"sign":%q}`, body, sign(body, key))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
	})}
	engines := []struct {
		name string
		dsn  string
	}{
		{"sqlite", ""},
		{"postgres", os.Getenv("PAYMENT_TEST_POSTGRES_DSN")},
		{"mysql", os.Getenv("PAYMENT_TEST_MYSQL_DSN")},
	}
	for _, engine := range engines {
		t.Run(engine.name, func(t *testing.T) {
			if engine.name != "sqlite" && engine.dsn == "" {
				t.Skip("isolated payment test database not configured")
			}
			oldDB, oldLogDB := model.DB, model.LOG_DB
			oldMainKind, oldLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			oldRedis, oldCache, oldMaster, oldSQLite := common.RedisEnabled, common.MemoryCacheEnabled, common.IsMasterNode, common.SQLitePath
			t.Cleanup(func() {
				model.DB, model.LOG_DB = oldDB, oldLogDB
				common.SetDatabaseTypes(oldMainKind, oldLogKind)
				common.RedisEnabled, common.MemoryCacheEnabled, common.IsMasterNode, common.SQLitePath = oldRedis, oldCache, oldMaster, oldSQLite
			})
			t.Setenv("SQL_DSN", engine.dsn)
			t.Setenv("LOG_SQL_DSN", "")
			common.RedisEnabled, common.MemoryCacheEnabled, common.IsMasterNode = false, false, false
			common.SQLitePath = filepath.Join(t.TempDir(), "payment.db")
			require.NoError(t, model.InitDB())
			require.NoError(t, model.InitLogDB())
			database := model.DB
			pool, err := database.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, pool.Close()) })
			for _, table := range []any{&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}} {
				require.False(t, database.Migrator().HasTable(table), "payment tests require an empty database")
			}
			require.NoError(t, database.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}, &model.SubscriptionPlan{}))
			for key, value := range options {
				require.NoError(t, database.Create(&model.Option{Key: key, Value: value}).Error)
			}
			common.OptionMap = map[string]string{}
			require.NoError(t, model.LoadOptionsFromDatabase())
			require.NoError(t, database.Create(&model.User{Id: 42, Username: "synthetic_wallet", Group: "default", Status: common.UserStatusEnabled, Quota: 300}).Error)
			router := gin.New()
			router.GET("/api/user/topup/info", GetTopUpInfo)
			router.POST("/api/user/pay", func(ctx *gin.Context) { ctx.Set("id", 42); RequestEpay(ctx) })
			router.POST("/api/user/alipay/notify", AlipayNotify)
			router.POST("/api/user/alipay/checkout", AlipayCheckout)
			router.GET("/api/option/alipay", GetDirectAlipaySettings)
			router.PUT("/api/option/alipay", SaveDirectAlipaySettings)
			router.GET("/api/option/", GetOptions)
			router.PUT("/api/option/", UpdateOption)
			router.GET("/api/subscription/plans", GetSubscriptionPlans)
			router.POST("/api/subscription/epay/pay", SubscriptionRequestEpay)
			call := func(method, path, contentType, content string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(method, path, strings.NewReader(content))
				request.Header.Set("Content-Type", contentType)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				return response
			}
			readInfo := func() (bool, []map[string]string) {
				var info struct {
					Data struct {
						Enabled bool                `json:"enable_online_topup"`
						Methods []map[string]string `json:"pay_methods"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(call(http.MethodGet, "/api/user/topup/info", "", "").Body.Bytes(), &info))
				return info.Data.Enabled, info.Data.Methods
			}
			before := precreates
			paymentSetting := operation_setting.GetPaymentSetting()
			paymentSetting.ComplianceConfirmed = false
			defer func() { paymentSetting.ComplianceConfirmed = true }()
			enabled, methods := readInfo()
			require.False(t, enabled)
			require.Empty(t, methods)
			submission := `{"amount":10,"payment_method":"alipay"}`
			require.NotContains(t, call(http.MethodPost, "/api/user/pay", "application/json", submission).Body.String(), "qr_code")
			require.Equal(t, before, precreates)
			paymentSetting.ComplianceConfirmed = true
			enabled, methods = readInfo()
			require.True(t, enabled)
			require.Equal(t, []map[string]string{{"type": "alipay"}}, methods)
			require.False(t, isEpayWebhookEnabled())
			require.NoError(t, database.Create(&model.SubscriptionPlan{Id: 11, Title: "Synthetic plan", Enabled: true, PriceAmount: 10}).Error)
			readPlanPayments := func() *subscriptionPaymentMethods {
				var result struct {
					Data []SubscriptionPlanDTO `json:"data"`
				}
				require.NoError(t, common.Unmarshal(call(http.MethodGet, "/api/subscription/plans", "", "").Body.Bytes(), &result))
				require.Len(t, result.Data, 1)
				require.NotNil(t, result.Data[0].Payment)
				return result.Data[0].Payment
			}
			require.Empty(t, readPlanPayments().Epay)
			require.Contains(t, call(http.MethodPost, "/api/subscription/epay/pay", "application/json", `{"plan_id":11,"payment_method":"alipay"}`).Body.String(), "not been configured")
			operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://epay.example", "epay-merchant", "fixture-secret"
			operation_setting.PayMethods = append(operation_setting.PayMethods, map[string]string{"type": "waffo_pancake"})
			require.Equal(t, []map[string]string{{"type": "alipay", "name": ""}, {"type": "wxpay", "name": ""}}, readPlanPayments().Epay)
			require.Contains(t, call(http.MethodPost, "/api/subscription/epay/pay", "application/json", `{"plan_id":11,"payment_method":"waffo_pancake"}`).Body.String(), "Payment method does not exist")
			operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "", "", ""
			var order struct {
				Message string `json:"message"`
				URL     string `json:"url"`
				Data    struct {
					TradeNo string `json:"trade_no"`
					QRCode  string `json:"qr_code"`
					Return  string `json:"return_url"`
				} `json:"data"`
			}
			response := call(http.MethodPost, "/api/user/pay", "application/json", submission)
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &order), response.Body.String())
			require.Equal(t, "success", order.Message)
			require.Equal(t, before+1, precreates)
			require.Equal(t, "/api/user/alipay/checkout", order.URL)
			require.Equal(t, "https://qr.alipay.com/synthetic-wallet", order.Data.QRCode)
			require.Equal(t, options["AlipayReturnURL"], order.Data.Return)
			pending := model.GetTopUpByTradeNo(order.Data.TradeNo)
			require.NotNil(t, pending)
			require.Equal(t, common.TopUpStatusPending, pending.Status)
			require.Equal(t, model.PaymentProviderAlipay, pending.PaymentProvider)
			retired := call(http.MethodPost, order.URL, "application/x-www-form-urlencoded", url.Values{"trade_no": {order.Data.TradeNo}}.Encode())
			require.Equal(t, http.StatusGone, retired.Code)
			require.NotContains(t, retired.Body.String(), "<img")
			notify := func(tradeNo, appID, amount string, key *rsa.PrivateKey, validSignature bool) string {
				values := url.Values{"app_id": {appID}, "out_trade_no": {tradeNo},
					"trade_no": {"provider-wallet-fixture"}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {amount}}
				fields := make([]string, 0, len(values))
				for key := range values {
					fields = append(fields, key+"="+values.Get(key))
				}
				slices.Sort(fields)
				signature := sign(strings.Join(fields, "&"), key)
				if !validSignature {
					signature = sign("different signed payload", key)
				}
				values.Set("sign", signature)
				values.Set("sign_type", "RSA2")
				return call(http.MethodPost, "/api/user/alipay/notify", "application/x-www-form-urlencoded", values.Encode()).Body.String()
			}
			appID := options["AlipayAppID"]
			tradeNo := order.Data.TradeNo
			require.Equal(t, "fail", notify(tradeNo, appID, "10.00", providerKey, false))
			require.Equal(t, "fail", notify(tradeNo, appID, "100.00", providerKey, true))
			require.Equal(t, "fail", notify(tradeNo, appID, "10.001", providerKey, true))
			require.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.Data.TradeNo).Status)
			admin := call(http.MethodGet, "/api/option/alipay", "", "")
			require.Equal(t, "no-store", admin.Header().Get("Cache-Control"))
			require.NotContains(t, admin.Body.String(), options["AlipayPrivateKey"])
			var view struct {
				Data alipayOptionsView `json:"data"`
			}
			require.NoError(t, common.Unmarshal(admin.Body.Bytes(), &view))
			require.True(t, view.Data.PrivateKeyConfigured)
			require.Equal(t, appID, view.Data.AppID)
			require.NotContains(t, call(http.MethodGet, "/api/option/", "", "").Body.String(), "AlipayPrivateKey")
			require.Contains(t, call(http.MethodPut, "/api/option/", "application/json", `{"key":"AlipayAppID","value":"other"}`).Body.String(), "Use the Alipay payment settings")
			draft := map[string]string{"app_id": appID, "public_key": options["AlipayPublicKey"], "notify_url": view.Data.NotifyURL, "return_url": view.Data.ReturnURL, "revision": view.Data.Revision}
			save := func() *httptest.ResponseRecorder {
				payload, err := common.Marshal(draft)
				require.NoError(t, err)
				return call(http.MethodPut, "/api/option/alipay", "application/json", string(payload))
			}
			beforeSave, err := model.GetDirectAlipayOptions()
			require.NoError(t, err)
			draft["notify_url"] = "http://merchant.example/not-https"
			require.Contains(t, save().Body.String(), "valid HTTPS URL")
			afterFailedSave, err := model.GetDirectAlipayOptions()
			require.NoError(t, err)
			require.Equal(t, beforeSave, afterFailedSave)
			draft["notify_url"] = view.Data.NotifyURL
			saved := save()
			require.Contains(t, saved.Body.String(), `"success":true`)
			require.Equal(t, http.StatusConflict, save().Code)
			require.NoError(t, common.Unmarshal(saved.Body.Bytes(), &view))
			stored, err := model.GetDirectAlipayOptions()
			require.NoError(t, err)
			require.Equal(t, options["AlipayPrivateKey"], stored["AlipayPrivateKey"])
			const legacyTrade = "ALIPAYUSR42SYNTHETIC0"
			require.NoError(t, database.Create(&model.TopUp{UserId: 42, Amount: 10, Money: 10, Status: common.TopUpStatusPending,
				TradeNo: legacyTrade, PaymentProvider: model.PaymentProviderAlipay, PaymentMethod: "alipay"}).Error)
			draft["app_id"], draft["revision"] = "synthetic-rotated", view.Data.Revision
			draft["public_key"] = base64.StdEncoding.EncodeToString(rotatedPublic)
			require.Contains(t, save().Body.String(), "private key for the new Alipay App ID")
			draft["private_key"] = options["AlipayPrivateKey"]
			require.Contains(t, save().Body.String(), `"success":true`)
			newOrder := call(http.MethodPost, "/api/user/pay", "application/json", submission)
			require.NoError(t, common.Unmarshal(newOrder.Body.Bytes(), &order), newOrder.Body.String())
			require.Equal(t, "success", order.Message)
			require.NotEqual(t, tradeNo, order.Data.TradeNo)
			require.Equal(t, "fail", notify(tradeNo, draft["app_id"], "10.00", rotatedProvider, true))
			require.Equal(t, "fail", notify(order.Data.TradeNo, appID, "10.00", providerKey, true))
			paymentSetting.ComplianceConfirmed = false
			require.Equal(t, "success", notify(tradeNo, appID, "10.00", providerKey, true))
			require.Equal(t, "success", notify(tradeNo, appID, "10.00", providerKey, true))
			require.Equal(t, "success", notify(legacyTrade, appID, "10.00", providerKey, true))
			require.Equal(t, "success", notify(order.Data.TradeNo, draft["app_id"], "10.00", rotatedProvider, true))
			var user model.User
			require.NoError(t, database.First(&user, 42).Error)
			require.Equal(t, 15000300, user.Quota)
			require.Equal(t, common.TopUpStatusSuccess, model.GetTopUpByTradeNo(order.Data.TradeNo).Status)
		})
	}
}

// epayCallbackCase is one callback sent to every Epay callback endpoint.
type epayCallbackCase struct {
	name         string
	params       map[string]string
	validPending bool
}

// setupEpayCallbackTest configures an Epay gateway and leaves model.DB nil:
// rejected callbacks and valid non-success events must never reach settlement,
// so an accidental attempt to settle fails the test.
func setupEpayCallbackTest(t *testing.T) {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	paymentSetting := operation_setting.GetPaymentSetting()
	originalSignType := paymentSetting.EpaySignType
	originalMerchantPrivateKey := paymentSetting.EpayMerchantPrivateKey
	originalPlatformPublicKey := paymentSetting.EpayPlatformPublicKey
	originalPayMethods := operation_setting.PayMethods
	originalServerAddress := system_setting.ServerAddress
	originalDB := model.DB
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		paymentSetting.EpaySignType = originalSignType
		paymentSetting.EpayMerchantPrivateKey = originalMerchantPrivateKey
		paymentSetting.EpayPlatformPublicKey = originalPlatformPublicKey
		operation_setting.PayMethods = originalPayMethods
		system_setting.ServerAddress = originalServerAddress
		model.DB = originalDB
	})
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	system_setting.ServerAddress = "https://dashboard.example.com"
	model.DB = nil
}

// assertEpayCallbacksVerifyBeforeSettlement sends each callback to the top-up
// and subscription endpoints by GET and POST. Only a valid pending callback is
// accepted; every other callback is rejected.
func assertEpayCallbacksVerifyBeforeSettlement(t *testing.T, cases []epayCallbackCase) {
	t.Helper()
	endpoints := []struct {
		name             string
		path             string
		handler          gin.HandlerFunc
		status           int
		rejectedBody     string
		pendingBody      string
		rejectedLocation string
		pendingLocation  string
	}{
		{
			name: "topup notify", path: "/api/user/epay/notify", handler: EpayNotify,
			status: http.StatusOK, rejectedBody: "fail", pendingBody: "success",
		},
		{
			name: "subscription notify", path: "/api/subscription/epay/notify", handler: SubscriptionEpayNotify,
			status: http.StatusOK, rejectedBody: "fail", pendingBody: "fail",
		},
		{
			name: "subscription return", path: "/api/subscription/epay/return", handler: SubscriptionEpayReturn,
			status:           http.StatusFound,
			rejectedLocation: "https://dashboard.example.com/wallet?pay=fail",
			pendingLocation:  "https://dashboard.example.com/wallet?pay=pending",
		},
	}
	router := gin.New()
	for _, endpoint := range endpoints {
		router.Any(endpoint.path, endpoint.handler)
	}
	for _, endpoint := range endpoints {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, tc := range cases {
				t.Run(endpoint.name+"/"+method+"/"+tc.name, func(t *testing.T) {
					params := url.Values{}
					for key, value := range tc.params {
						params.Set(key, value)
					}
					request := httptest.NewRequest(method, endpoint.path+"?"+params.Encode(), nil)
					if method == http.MethodPost {
						request = httptest.NewRequest(method, endpoint.path, strings.NewReader(params.Encode()))
						request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					}
					recorder := httptest.NewRecorder()
					require.NotPanics(t, func() { router.ServeHTTP(recorder, request) })
					assert.Equal(t, endpoint.status, recorder.Code)
					wantBody, wantLocation := endpoint.rejectedBody, endpoint.rejectedLocation
					if tc.validPending {
						wantBody, wantLocation = endpoint.pendingBody, endpoint.pendingLocation
					}
					if wantBody != "" {
						assert.Equal(t, wantBody, recorder.Body.String())
					}
					assert.Equal(t, wantLocation, recorder.Header().Get("Location"))
				})
			}
		}
	}
}

// newEpayRSAKeyPairForTest returns a throwaway key pair as single-line Base64
// text, the format Epay merchant consoles show.
func newEpayRSAKeyPairForTest(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(privateDER), base64.StdEncoding.EncodeToString(publicDER)
}

func TestEpayCallbacksVerifyBeforeSettlement(t *testing.T) {
	setupEpayCallbackTest(t)
	paymentSetting := operation_setting.GetPaymentSetting()
	paymentSetting.EpaySignType = operation_setting.EpaySignTypeMD5
	operation_setting.EpayKey = "test-key"
	require.True(t, isEpayWebhookEnabled())

	// Both ambiguous maps below had this same valid signing string in v0.0.4.
	// This must fail because of parameter ambiguity, not a mismatched signature.
	ambiguousSign := epay.MD5String("money=1.00&name=item&out_trade_no=other&out_trade_no=order&pid=epay_id&trade_status=TRADE_SUCCESS&type=alipay", "test-key")
	pendingSign := epay.MD5String("money=1.00&name=item%26other&out_trade_no=order&pid=epay_id&trade_status=WAIT_BUYER_PAY&type=alipay", "test-key")
	rows := []struct {
		name         string
		productName  string
		tradeNo      string
		status       string
		sign         string
		validPending bool
	}{
		{"separator in product name", "item&out_trade_no=other", "order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"separator in order number", "item", "other&out_trade_no=order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"invalid signature", "item", "order", epay.StatusTradeSuccess, "invalid", false},
		{"missing signature", "item", "order", epay.StatusTradeSuccess, "", false},
		{"valid pending with literal percent escape", "item%26other", "order", "WAIT_BUYER_PAY", pendingSign, true},
	}
	cases := make([]epayCallbackCase, 0, len(rows))
	for _, row := range rows {
		params := map[string]string{
			"pid": "epay_id", "type": "alipay", "money": "1.00",
			"name": row.productName, "out_trade_no": row.tradeNo,
			"trade_status": row.status, "sign_type": "MD5",
		}
		if row.sign != "" {
			params["sign"] = row.sign
		}
		cases = append(cases, epayCallbackCase{name: row.name, params: params, validPending: row.validPending})
	}
	assertEpayCallbacksVerifyBeforeSettlement(t, cases)
}

func TestEzpayCheckoutAndCallback(t *testing.T) {
	setupEpayCallbackTest(t)
	operation_setting.GetPaymentSetting().EpaySignType = operation_setting.EpaySignTypeEzpay
	operation_setting.EpayKey = "test-key"
	require.True(t, isEpayWebhookEnabled())
	require.NoError(t, operation_setting.ValidateEpaySignOption(operation_setting.EpaySignTypeOptionKey, operation_setting.EpaySignTypeEzpay))
	client := GetEpayClient()
	require.NotNil(t, client)
	notifyURL, err := url.Parse("https://dashboard.example.com/api/user/epay/notify")
	require.NoError(t, err)
	returnURL, err := url.Parse("https://dashboard.example.com/usage-logs")
	require.NoError(t, err)
	for method, payType := range map[string]string{operation_setting.EpayAlipayMethod: "alipay", "wxpay": "wxpay", "usdt_trc20": "usdt_trc20"} {
		endpoint, order, err := purchaseEpay(client, epay.PurchaseArgs{
			Type: method, ServiceTradeNo: "order", Name: "TUC1", Money: "1.00",
			Device: epay.PC, NotifyUrl: notifyURL, ReturnUrl: returnURL,
		}, "CNY")
		require.NoError(t, err)
		assert.Equal(t, "https://pay.example.com/submit.php", endpoint)
		assert.NotContains(t, order, "device")
		assert.Equal(t, "CNY", order["currency"])
		assert.Equal(t, payType, order["type"])
		assert.Equal(t, epay.SignTypeMD5, order["sign_type"])
		expected := epay.MD5String("currency=CNY&money=1.00&name=TUC1&notify_url=https://dashboard.example.com/api/user/epay/notify&out_trade_no=order&pid=epay_id&return_url=https://dashboard.example.com/usage-logs&type="+payType, "test-key")
		assert.Equal(t, expected, order["sign"])
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		require.NoError(t, sqlDB.Close())
	})
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, db.Create(&model.User{Id: 53, Username: "ezpay-user"}).Error)
	for tradeNo, provider := range map[string]string{"ezpay-order": model.PaymentProviderEpay, "direct-order": model.PaymentProviderAlipay} {
		require.NoError(t, db.Create(&model.TopUp{
			TradeNo: tradeNo, UserId: 53, Amount: 1, Money: 1,
			PaymentMethod: "alipay", PaymentProvider: provider, Status: common.TopUpStatusPending,
		}).Error)
	}
	router := gin.New()
	router.GET(notifyURL.Path, EpayNotify)
	for _, notification := range []struct{ tradeNo, currency, money, result string }{
		{"direct-order", "CNY", "1.00", "fail"},
		{"ezpay-order", "USD", "1.00", "fail"},
		{"ezpay-order", "CNY", "0.99", "fail"},
		{"ezpay-order", "CNY", "1.00", "success"},
		{"ezpay-order", "CNY", "1.00", "success"},
	} {
		fields, err := client.SignParams(map[string]string{
			"trade_status": epay.StatusTradeSuccess, "out_trade_no": notification.tradeNo,
			"money": notification.money, "currency": notification.currency, "type": "alipay",
			"pay_currency": "CNY", "pay_amount": "1.00",
		})
		require.NoError(t, err)
		query := url.Values{}
		for key, value := range fields {
			query.Set(key, value)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, notifyURL.Path+"?"+query.Encode(), nil))
		assert.Equal(t, notification.result, response.Body.String())
	}
	var user model.User
	require.NoError(t, db.First(&user, 53).Error)
	assert.Equal(t, int(common.QuotaPerUnit), user.Quota)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo("direct-order").Status)
	assert.Equal(t, common.TopUpStatusSuccess, model.GetTopUpByTradeNo("ezpay-order").Status)
}

func TestEpayRSAModeSignsOrdersAndVerifiesCallbacks(t *testing.T) {
	setupEpayCallbackTest(t)
	paymentSetting := operation_setting.GetPaymentSetting()
	merchantPrivateKey, merchantPublicKey := newEpayRSAKeyPairForTest(t)
	platformPrivateKey, platformPublicKey := newEpayRSAKeyPairForTest(t)
	paymentSetting.EpaySignType = operation_setting.EpaySignTypeRSA
	// A leftover MD5 key must not be accepted once RSA is selected.
	operation_setting.EpayKey = "test-key"
	paymentSetting.EpayMerchantPrivateKey = merchantPrivateKey
	paymentSetting.EpayPlatformPublicKey = platformPublicKey
	require.True(t, isEpayWebhookEnabled())

	client := GetEpayClient()
	require.NotNil(t, client)
	notifyURL, err := url.Parse("https://dashboard.example.com/api/user/epay/notify")
	require.NoError(t, err)
	returnURL, err := url.Parse("https://dashboard.example.com/usage-logs")
	require.NoError(t, err)
	paymentURL, order, err := client.Purchase(&epay.PurchaseArgs{
		Type: "alipay", ServiceTradeNo: "order", Name: "TUC1", Money: "1.00",
		Device: epay.PC, NotifyUrl: notifyURL, ReturnUrl: returnURL,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/api/pay/submit", paymentURL)
	assert.Equal(t, epay.SignTypeRSA, order["sign_type"])

	// The platform side holds the merchant public key and signs callbacks with its own private key.
	platform, err := epay.NewClient(&epay.Config{
		PartnerID: "epay_id", SignType: epay.SignTypeRSA,
		MerchantPrivateKey: platformPrivateKey, PlatformPublicKey: merchantPublicKey,
	}, "https://pay.example.com")
	require.NoError(t, err)
	verifiedOrder, err := platform.Verify(order)
	require.NoError(t, err)
	assert.True(t, verifiedOrder.VerifyStatus)

	pending, err := platform.SignParams(map[string]string{
		"type": "alipay", "money": "1.00", "name": "item", "out_trade_no": "order", "trade_status": "WAIT_BUYER_PAY",
	})
	require.NoError(t, err)
	paidFields := map[string]string{
		"type": "alipay", "money": "1.00", "name": "item", "out_trade_no": "order", "trade_status": epay.StatusTradeSuccess,
	}
	paid, err := platform.SignParams(paidFields)
	require.NoError(t, err)
	tampered := maps.Clone(paid)
	tampered["money"] = "100.00"
	merchantSigned, err := client.SignParams(paidFields)
	require.NoError(t, err)
	md5Client, err := epay.NewClient(&epay.Config{PartnerID: "epay_id", Key: "test-key"}, "https://pay.example.com")
	require.NoError(t, err)
	md5Signed, err := md5Client.SignParams(paidFields)
	require.NoError(t, err)
	unsigned := maps.Clone(paid)
	delete(unsigned, "sign")

	assertEpayCallbacksVerifyBeforeSettlement(t, []epayCallbackCase{
		{name: "valid pending signed by platform", params: pending, validPending: true},
		{name: "tampered amount", params: tampered},
		{name: "signed with merchant key", params: merchantSigned},
		{name: "MD5 signature in RSA mode", params: md5Signed},
		{name: "missing signature", params: unsigned},
	})
}

func TestUpdateOptionValidatesEpaySignSettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	originalOptionMap := common.OptionMap
	paymentSetting := operation_setting.GetPaymentSetting()
	originalSignType := paymentSetting.EpaySignType
	originalMerchantPrivateKey := paymentSetting.EpayMerchantPrivateKey
	originalPlatformPublicKey := paymentSetting.EpayPlatformPublicKey
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
		paymentSetting.EpaySignType = originalSignType
		paymentSetting.EpayMerchantPrivateKey = originalMerchantPrivateKey
		paymentSetting.EpayPlatformPublicKey = originalPlatformPublicKey
	})
	// A site upgraded from an MD5-only release has no saved RSA settings.
	common.OptionMap = map[string]string{
		operation_setting.EpaySignTypeOptionKey:           operation_setting.EpaySignTypeMD5,
		operation_setting.EpayMerchantPrivateKeyOptionKey: "",
		operation_setting.EpayPlatformPublicKeyOptionKey:  "",
	}
	paymentSetting.EpaySignType = operation_setting.EpaySignTypeMD5
	paymentSetting.EpayMerchantPrivateKey = ""
	paymentSetting.EpayPlatformPublicKey = ""
	signTypeKey := operation_setting.EpaySignTypeOptionKey
	privateKeyKey := operation_setting.EpayMerchantPrivateKeyOptionKey
	publicKeyKey := operation_setting.EpayPlatformPublicKeyOptionKey
	merchantPrivateKey, _ := newEpayRSAKeyPairForTest(t)
	_, platformPublicKey := newEpayRSAKeyPairForTest(t)
	const privateKeyError = "The Epay merchant private key is missing or invalid"
	const publicKeyError = "The Epay platform public key is missing or invalid"

	steps := []struct {
		name      string
		key       string
		value     string
		wantError string
	}{
		{"unknown signature type", signTypeKey, "SHA1", "Epay signature type must be MD5, RSA, or ezpay (MD5)"},
		{"RSA before keys are saved", signTypeKey, "RSA", privateKeyError},
		{"malformed private key", privateKeyKey, "not a key", privateKeyError},
		{"public key in the private key field", privateKeyKey, platformPublicKey, privateKeyError},
		{"save private key", privateKeyKey, merchantPrivateKey, ""},
		{"RSA before the public key is saved", signTypeKey, "RSA", publicKeyError},
		{"private key in the public key field", publicKeyKey, merchantPrivateKey, publicKeyError},
		{"save public key", publicKeyKey, platformPublicKey, ""},
		{"switch to RSA", signTypeKey, "RSA", ""},
		{"clear private key while RSA is active", privateKeyKey, "", privateKeyError},
		{"switch back to MD5", signTypeKey, "MD5", ""},
	}
	for _, step := range steps {
		err := model.UpdateOption(step.key, step.value)
		if step.wantError == "" {
			require.NoError(t, err, step.name)
			continue
		}
		require.Error(t, err, step.name)
		assert.Equal(t, step.wantError, err.Error(), step.name)
	}
	assert.Equal(t, operation_setting.EpaySignTypeMD5, paymentSetting.EpaySignType)
	assert.Equal(t, merchantPrivateKey, paymentSetting.EpayMerchantPrivateKey)
	assert.Equal(t, platformPublicKey, paymentSetting.EpayPlatformPublicKey)

	// The private key is never sent back to the settings page; the public key may be.
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(context)
	var response struct {
		Success bool           `json:"success"`
		Data    []model.Option `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	returned := make(map[string]string, len(response.Data))
	for _, option := range response.Data {
		returned[option.Key] = option.Value
	}
	assert.Equal(t, operation_setting.EpaySignTypeMD5, returned[signTypeKey])
	assert.Equal(t, platformPublicKey, returned[publicKeyKey])
	assert.NotContains(t, returned, privateKeyKey)
}
