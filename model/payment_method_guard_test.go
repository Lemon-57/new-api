package model

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) *User {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

// External DSNs are opt-in and restricted to the disposable loopback fixture.
// Run this test again in a new process to exercise durable retry state.
func TestAlipayOrderDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			for _, startingState := range []string{"fresh", "upgrade"} {
				t.Run(startingState, func(t *testing.T) {
					db := alipayAccountingTestDB(t, engine, startingState)
					if startingState == "upgrade" {
						require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &AlipayOrder{}, &AlipayRefundRecord{}, &SubscriptionPlan{}, &SubscriptionOrder{}, &UserSubscription{}, &Log{}))
						legacy := User{Username: "legacy-alipay", AffCode: "legacy-test", Quota: 91, Status: common.UserStatusEnabled}
						require.NoError(t, db.Create(&legacy).Error)
						require.NoError(t, db.Create(&TopUp{UserId: legacy.Id, TradeNo: "legacy-order", Amount: 1, Money: 1,
							PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess}).Error)
					}
					for range 2 {
						require.NoError(t, migrateDB(), "actual startup migration must be repeatable")
					}
					if startingState == "upgrade" {
						var legacy User
						require.NoError(t, db.Where("username = ?", "legacy-alipay").First(&legacy).Error)
						require.Equal(t, 91, legacy.Quota)
						require.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo("legacy-order").Status)
					}
					var version string
					query := "SELECT version()"
					if engine == "sqlite" {
						query = "SELECT sqlite_version()"
					}
					require.NoError(t, db.Raw(query).Scan(&version).Error)
					t.Logf("database=%s version=%s starting_state=%s startup_migrations=2", engine, version, startingState)
					alipayAccountingCases(t, db)
				})
			}
		})
	}
}

func alipayAccountingTestDB(t *testing.T, engine, startingState string) *gorm.DB {
	t.Helper()
	var dialect gorm.Dialector
	targetDB := "alipay_" + startingState
	switch engine {
	case "sqlite":
		dialect = sqlite.Open(filepath.Join(t.TempDir(), "alipay.db"))
	case "postgres":
		raw := os.Getenv("ALIPAY_TEST_POSTGRES_DSN")
		if raw == "" {
			t.Skip("ALIPAY_TEST_POSTGRES_DSN not set; real PostgreSQL matrix not run")
		}
		u, err := url.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", u.Hostname(), "database test must stay local")
		require.Equal(t, "/alipay_fixture", u.Path)
		u.Path = "/" + targetDB
		dialect = postgres.Open(u.String())
	case "mysql":
		raw := os.Getenv("ALIPAY_TEST_MYSQL_DSN")
		if raw == "" {
			t.Skip("ALIPAY_TEST_MYSQL_DSN not set; real MySQL matrix not run")
		}
		require.Contains(t, raw, "@tcp(127.0.0.1:49317)/alipay_fixture?")
		dialect = mysql.Open(strings.Replace(raw, "/alipay_fixture?", "/"+targetDB+"?", 1))
	}
	db, err := gorm.Open(dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if engine == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
	} else {
		sqlDB.SetMaxOpenConns(4)
	}
	oldDB, oldLogDB := DB, LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseType(engine), common.DatabaseType(engine))
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		initCol()
		_ = sqlDB.Close()
	})
	// Dedicated fixture databases contain no user data. Remove earlier fixture
	// tables so both the fresh and released-schema upgrade paths are exercised.
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	for _, table := range tables {
		require.NoError(t, db.Migrator().DropTable(table))
	}
	return db
}

func alipayAccountingCases(t *testing.T, db *gorm.DB) {
	t.Helper()
	user := &User{Username: "alipay-accounting", AffCode: "accounting-test", Quota: 10, Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order := &AlipayOrder{UserId: user.Id, RequestId: "wallet-request", OrderId: "wallet-order", AppId: "test-app", SellerId: "test-seller",
		AmountCents: 288, CreditedQuota: 123456, RequestedAmount: 1}
	require.NoError(t, CreateAlipayOrder(order, nil, 1))
	// The persisted request identity prevents two checkout attempts from issuing
	// two external order numbers for the same purchase intent.
	duplicate := *order
	duplicate.OrderId = "wallet-duplicate"
	require.Error(t, CreateAlipayOrder(&duplicate, nil, 1))
	recovered, err := GetAlipayOrderByRequest(user.Id, order.RequestId)
	require.NoError(t, err)
	require.Equal(t, order.OrderId, recovered.OrderId)
	payment := pay.Payment{OrderID: order.OrderId, TradeNo: "ali-wallet", AmountCents: 288, State: pay.Paid}
	for _, mismatch := range []pay.Payment{
		{OrderID: order.OrderId, TradeNo: "ali-wallet", AmountCents: 287, State: pay.Paid},
		{OrderID: order.OrderId, TradeNo: "ali-wallet", AmountCents: 288, State: pay.Pending},
	} {
		_, err := CompleteAlipayOrder(mismatch, order.AppId, order.SellerId, "127.0.0.1")
		require.Error(t, err)
	}
	_, err = CompleteAlipayOrder(payment, "foreign-app", order.SellerId, "127.0.0.1")
	require.Error(t, err)
	// Simultaneous duplicate callbacks represent separate request transactions.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := CompleteAlipayOrder(payment, order.AppId, order.SellerId, "127.0.0.1")
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, 123466, getUserQuotaForPaymentGuardTest(t, user.Id))
	// A subsequent request models a lost acknowledgment after the first commit.
	already, err := CompleteAlipayOrder(payment, order.AppId, order.SellerId, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, already)
	require.Equal(t, 123466, getUserQuotaForPaymentGuardTest(t, user.Id))
	require.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo(order.OrderId).Status)
	refundRecord, refundRequest, err := PrepareAlipayRefund(order.OrderId, 100, "local reconciliation")
	require.NoError(t, err)
	require.Equal(t, AlipayRefundPending, refundRecord.Status)
	require.Equal(t, refundRecord.RefundRequestId, refundRequest.RefundRequestID)
	retryRecord, retryRequest, err := PrepareAlipayRefund(order.OrderId, 100, "local reconciliation")
	require.NoError(t, err)
	require.Equal(t, refundRecord.RefundRequestId, retryRecord.RefundRequestId)
	require.Equal(t, refundRequest.RefundRequestID, retryRequest.RefundRequestID)
	_, _, err = PrepareAlipayRefund(order.OrderId, 101, "local reconciliation")
	require.ErrorIs(t, err, ErrAlipayRefundMismatch)
	require.NoError(t, SetAlipayRefundState(refundRecord.RefundRequestId, AlipayRefunded, `{"fixture":true}`))
	require.NoError(t, SetAlipayRefundState(refundRecord.RefundRequestId, AlipayRefunded, ""))
	require.ErrorIs(t, SetAlipayRefundState(refundRecord.RefundRequestId, AlipayRefundFailed, ""), ErrAlipayRefundState)
	second := &AlipayOrder{UserId: user.Id, RequestId: "second-request", OrderId: "second-order", AppId: order.AppId, SellerId: order.SellerId,
		AmountCents: 288, CreditedQuota: 1, RequestedAmount: 1}
	require.NoError(t, CreateAlipayOrder(second, nil, 1))
	payment.OrderID = second.OrderId
	_, err = CompleteAlipayOrder(payment, second.AppId, second.SellerId, "127.0.0.1")
	require.Error(t, err, "a provider transaction number cannot pay for two orders")
	require.Equal(t, 123466, getUserQuotaForPaymentGuardTest(t, user.Id))
	// Failure after the quota increment (native receipt mismatch) rolls back
	// both the balance and the transaction-number binding.
	require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", second.OrderId).Update("payment_provider", PaymentProviderEpay).Error)
	payment.TradeNo = "ali-second"
	_, err = CompleteAlipayOrder(payment, second.AppId, second.SellerId, "127.0.0.1")
	require.Error(t, err)
	require.Equal(t, 123466, getUserQuotaForPaymentGuardTest(t, user.Id))
	recovered, err = GetAlipayOrder(second.OrderId)
	require.NoError(t, err)
	require.Nil(t, recovered.TradeNo)
	require.Equal(t, common.TopUpStatusPending, recovered.Status)
	// Reconcile after resolving the native receipt, without recreating an order.
	require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", second.OrderId).Update("payment_provider", PaymentProviderAlipay).Error)
	_, err = CompleteAlipayOrder(payment, second.AppId, second.SellerId, "127.0.0.1")
	require.NoError(t, err)
	require.Equal(t, 123467, getUserQuotaForPaymentGuardTest(t, user.Id))
	ceiling := &AlipayOrder{UserId: user.Id, RequestId: "ceiling-request", OrderId: "ceiling-order", AppId: order.AppId, SellerId: order.SellerId,
		AmountCents: 1, CreditedQuota: 1, RequestedAmount: 1}
	require.NoError(t, CreateAlipayOrder(ceiling, nil, 1))
	require.NoError(t, db.Model(user).Update("quota", common.MaxWalletQuota).Error)
	_, err = CompleteAlipayOrder(pay.Payment{OrderID: ceiling.OrderId, TradeNo: "ali-ceiling", AmountCents: 1, State: pay.Paid}, ceiling.AppId, ceiling.SellerId, "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
	recovered, err = GetAlipayOrder(ceiling.OrderId)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, recovered.Status)
	require.Nil(t, recovered.TradeNo)
	require.Equal(t, common.MaxWalletQuota, getUserQuotaForPaymentGuardTest(t, user.Id))
	require.NoError(t, db.Model(user).Update("quota", 123467).Error)
	plan := &SubscriptionPlan{Title: "Snapshot", Enabled: true, PriceAmount: 2.88, Currency: "CNY", DurationUnit: SubscriptionDurationMonth,
		DurationValue: 1, TotalAmount: 1600, UpgradeGroup: "pro", MaxPurchasePerUser: 1}
	require.NoError(t, db.Create(plan).Error)
	planOrder := &AlipayOrder{UserId: user.Id, RequestId: "plan-request", OrderId: "plan-order", AppId: order.AppId, SellerId: order.SellerId,
		AmountCents: 288, PlanId: plan.Id}
	require.NoError(t, CreateAlipayOrder(planOrder, plan, 0))
	reserved := *planOrder
	reserved.RequestId, reserved.OrderId = "plan-over-limit", "plan-over-limit"
	require.Error(t, CreateAlipayOrder(&reserved, plan, 0), "pending purchases reserve the plan purchase limit")
	require.NoError(t, db.Model(plan).Updates(map[string]any{"total_amount": 9999, "upgrade_group": "changed-after-checkout"}).Error)
	paidPlan := pay.Payment{OrderID: planOrder.OrderId, TradeNo: "ali-plan", AmountCents: 288, State: pay.Paid}
	for range 2 {
		_, err := CompleteAlipayOrder(paidPlan, planOrder.AppId, planOrder.SellerId, "127.0.0.1")
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, countUserSubscriptionsForPaymentGuardTest(t, user.Id))
	var sub UserSubscription
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&sub).Error)
	require.EqualValues(t, 1600, sub.AmountTotal)
	require.Equal(t, "pro", sub.UpgradeGroup)
	require.Equal(t, "default", sub.PrevUserGroup)
	require.Equal(t, 123467, getUserQuotaForPaymentGuardTest(t, user.Id))
	require.Equal(t, PaymentProviderAlipay, GetTopUpByTradeNo(planOrder.OrderId).PaymentProvider)
	// The new schema survives a later process' startup path as well.
	require.NoError(t, migrateDB())
	already, err = CompleteAlipayOrder(paidPlan, planOrder.AppId, planOrder.SellerId, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, already)
	require.EqualValues(t, 1, countUserSubscriptionsForPaymentGuardTest(t, user.Id))
	t.Log(fmt.Sprintf("credited_once=%d subscription_count=1 duplicate_transaction_rejected=true native_receipt_failure_rolled_back=true", 123456))
	foreign := User{Username: "alipay-other", AffCode: "alipay-other", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&foreign).Error)
	require.NoError(t, CreateAlipayOrder(&AlipayOrder{UserId: foreign.Id, RequestId: "other", OrderId: "other", AppId: order.AppId, AmountCents: 1, CreditedQuota: 1, RequestedAmount: 1}, nil, 1))
	recent, err := ListRecentAlipayOrders(user.Id)
	require.NoError(t, err)
	require.Len(t, recent, 4)
	require.Equal(t, planOrder.OrderId, recent[0].OrderId)
	for _, item := range recent {
		require.Equal(t, user.Id, item.UserId)
	}
}

func TestAlipayWalletCreditKeepsCacheInSync(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&AlipayOrder{}))
	useUserCacheMiniRedis(t)
	user := insertUserForPaymentGuardTest(t, 1009, 10)
	user.AuthVersion = 1
	require.NoError(t, DB.Model(user).Update("auth_version", 1).Error)
	require.NoError(t, populateUserCache(*user))
	order := &AlipayOrder{UserId: user.Id, RequestId: "cache-request", OrderId: "cache-order", AppId: "test-app", AmountCents: 1, CreditedQuota: 77, RequestedAmount: 1}
	t.Cleanup(func() { DB.Where("order_id = ?", order.OrderId).Delete(&AlipayOrder{}) })
	require.NoError(t, CreateAlipayOrder(order, nil, 1))
	// Existing in-flight reservations must be retained while credit is added.
	result, err := cacheTryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	payment := pay.Payment{OrderID: order.OrderId, TradeNo: "ali-cache", AmountCents: 1, State: pay.Paid}
	for range 2 {
		_, err := CompleteAlipayOrder(payment, order.AppId, "", "127.0.0.1")
		require.NoError(t, err)
	}
	cache, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 84, cache.Quota, "duplicate callback must neither credit twice nor erase an in-flight reservation")
	require.Equal(t, 87, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestAlipayOrderProcessRecovery(t *testing.T) {
	phase := os.Getenv("ALIPAY_RECOVERY_PHASE")
	if phase == "" {
		path := filepath.Join(t.TempDir(), "alipay-recovery.db")
		executable, err := os.Executable()
		require.NoError(t, err)
		for _, phase := range []string{"commit-before-reply", "retry"} {
			command := exec.Command(executable, "-test.run", "^TestAlipayOrderProcessRecovery$", "-test.v")
			command.Env = append(os.Environ(), "ALIPAY_RECOVERY_PHASE="+phase, "ALIPAY_RECOVERY_PATH="+path)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Logf("new_process_phase=%s result=%s", phase, strings.TrimSpace(string(output)))
		}
		return
	}
	path := os.Getenv("ALIPAY_RECOVERY_PATH")
	require.True(t, filepath.IsAbs(path))
	require.Equal(t, "alipay-recovery.db", filepath.Base(path))
	db, err := gorm.Open(sqlite.Open(path+"?_txlock=immediate"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &AlipayOrder{}, &Log{}))
	payment := pay.Payment{OrderID: "recover-order", TradeNo: "recover-transaction", AmountCents: 1, State: pay.Paid}
	if phase == "commit-before-reply" {
		user := User{Username: "alipay-recovery", AffCode: "alipay-recovery", Status: common.UserStatusEnabled}
		require.NoError(t, db.Create(&user).Error)
		require.NoError(t, CreateAlipayOrder(&AlipayOrder{UserId: user.Id, RequestId: "recover-request", OrderId: payment.OrderID,
			AppId: "test-app", AmountCents: 1, CreditedQuota: 77, RequestedAmount: 1}, nil, 1))
		already, err := CompleteAlipayOrder(payment, "test-app", "", "127.0.0.1")
		require.NoError(t, err)
		require.False(t, already)
		fmt.Println("committed_once; exiting before caller acknowledgement")
		os.Exit(0) // Deliberately omit connection close and a response to the caller.
	}
	require.Equal(t, "retry", phase)
	already, err := CompleteAlipayOrder(payment, "test-app", "", "127.0.0.1")
	require.NoError(t, err)
	require.True(t, already)
	order, err := GetAlipayOrder(payment.OrderID)
	require.NoError(t, err)
	require.Equal(t, 77, getUserQuotaForPaymentGuardTest(t, order.UserId))
	var receipts int64
	require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", payment.OrderID).Count(&receipts).Error)
	require.EqualValues(t, 1, receipts)
}

func insertSubscriptionPlanForPaymentGuardTest(t *testing.T, id int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         "Guard Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertSubscriptionOrderForPaymentGuardTest(t *testing.T, tradeNo string, userID int, planID int, paymentProvider string) {
	t.Helper()
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          planID,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func countUserSubscriptionsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake("waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 150, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func TestCompleteSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 202, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 301)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-guard-order", 202, plan.Id, PaymentProviderStripe)

	err := CompleteSubscriptionOrder("sub-guard-order", `{"provider":"epay"}`, PaymentProviderEpay, "alipay")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-guard-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 202))

	topUp := GetTopUpByTradeNo("sub-guard-order")
	assert.Nil(t, topUp)
}

func TestExpireSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 303, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 401)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-expire-guard", 303, plan.Id, PaymentProviderStripe)

	err := ExpireSubscriptionOrder("sub-expire-guard", PaymentProviderCreem)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-expire-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
}

func createEpayTestOrder(t *testing.T, userId int, tradeNo string, provider string, status string) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:          userId,
		Amount:          2,
		Money:           10.0,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          status,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return topUp
}

func TestRechargeEpayCreditsQuotaExactlyOnce(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 501, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTONCE", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.NotZero(t, reloaded.CompleteTime)

	alreadyDone, err = RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayKeepsRedisAndDatabaseCreditInSync(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 5
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 502, 7)
	require.NoError(t, populateUserCache(*user))
	order := createEpayTestOrder(t, user.Id, "EPAYTESTREDISSYNC", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 17, getUserQuotaForPaymentGuardTest(t, user.Id))
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)

	alreadyDone, err = RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	cached, err = cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)
}

func TestRechargeEpayUpdatesPaymentMethodToActual(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 503, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTMETHOD", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(order.TradeNo, "wxpay", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, "wxpay", reloaded.PaymentMethod)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayRejectsForeignAndNonPendingOrders(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 504, 7)

	t.Run("order from another payment provider", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTSTRIPE", PaymentProviderStripe, common.TopUpStatusPending)
		_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
		assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
		assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("order that is not pending", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTEXPIRED", PaymentProviderEpay, common.TopUpStatusExpired)
		_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
		assert.ErrorIs(t, err, ErrTopUpStatusInvalid)
		assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("missing order", func(t *testing.T) {
		_, err := RechargeEpay("EPAYTESTMISSING", "alipay", "127.0.0.1")
		assert.ErrorIs(t, err, ErrTopUpNotFound)
	})
}

func TestRechargeEpayRejectsQuotaOverflowBeforeCompletingOrder(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = float64(common.MaxWalletQuota + 1)
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 505, 3)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTOVERFLOW", PaymentProviderEpay, common.TopUpStatusPending)

	_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.Error(t, err)
	assert.Equal(t, 3, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

func TestRechargeEpayEnforcesFinalWalletQuotaLimit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	testCases := []struct {
		name         string
		currentQuota int
		wantErr      bool
		wantQuota    int
		wantStatus   string
	}{
		{
			name:         "allows exact highest representable wallet balance",
			currentQuota: common.MaxWalletQuota - 1_000_000,
			wantQuota:    common.MaxWalletQuota,
			wantStatus:   common.TopUpStatusSuccess,
		},
		{
			name:         "rejects balance above wallet quota domain",
			currentQuota: common.MaxWalletQuota - 999_999,
			wantErr:      true,
			wantQuota:    common.MaxWalletQuota - 999_999,
			wantStatus:   common.TopUpStatusPending,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			user := insertUserForPaymentGuardTest(t, 506, tc.currentQuota)
			order := createEpayTestOrder(t, user.Id, "EPAYTESTWALLETLIMIT", PaymentProviderEpay, common.TopUpStatusPending)

			_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
			if tc.wantErr {
				require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantQuota, getUserQuotaForPaymentGuardTest(t, user.Id))
			assert.Equal(t, tc.wantStatus, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
		})
	}
}
