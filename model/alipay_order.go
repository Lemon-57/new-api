package model

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	dblogger "gorm.io/gorm/logger"
)

const PaymentProviderAlipay = "alipay_direct"

const (
	AlipayOrderClosing = "closing"
	AlipayOrderClosed  = "closed"
)

var alipayRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// AlipayOrder stores the facts agreed before contacting Alipay. In particular,
// neither a callback nor a subsequent pricing/settings change determines credit.
// The nullable unique transaction number prevents one payment fulfilling two
// merchant orders while permitting multiple pending orders on every dialect.
type AlipayOrder struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"not null;uniqueIndex:idx_alipay_request,priority:1"`
	RequestId       string  `json:"request_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_alipay_request,priority:2"`
	OrderId         string  `json:"order_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	AppId           string  `json:"-" gorm:"type:varchar(64);not null"`
	SellerId        string  `json:"-" gorm:"type:varchar(64)"`
	AmountCents     int64   `json:"amount_cents" gorm:"not null"`
	CreditedQuota   int     `json:"credited_quota" gorm:"not null"`
	RequestedAmount int64   `json:"requested_amount" gorm:"not null"`
	PlanId          int     `json:"plan_id"`
	PlanSnapshot    string  `json:"-" gorm:"type:text"`
	SubscriptionId  int     `json:"subscription_id"`
	TradeNo         *string `json:"trade_no,omitempty" gorm:"type:varchar(64);uniqueIndex"`
	Status          string  `json:"status" gorm:"type:varchar(32);not null"`
	CreatedAt       int64   `json:"created_at" gorm:"not null"`
	CompletedAt     int64   `json:"completed_at"`
}

// CreateAlipayOrder atomically creates both the snapshot and native order. A
// caller must look up a repeated request first and reuse it, never call Alipay
// with a new order number after an uncertain response.
// walletReceiptAmount uses native currency units even if RequestedAmount uses
// token display units; fulfillment always uses the immutable CreditedQuota.
func CreateAlipayOrder(order *AlipayOrder, plan *SubscriptionPlan, walletReceiptAmount int64) error {
	if order == nil || order.UserId <= 0 || order.AppId == "" ||
		!alipayRequestIDPattern.MatchString(order.RequestId) || !alipayRequestIDPattern.MatchString(order.OrderId) {
		return errors.New("invalid alipay order")
	}
	if _, err := pay.FormatAmount(order.AmountCents); err != nil {
		return err
	}
	if order.PlanId == 0 {
		if plan != nil || order.RequestedAmount <= 0 || walletReceiptAmount <= 0 {
			return errors.New("invalid alipay wallet order")
		}
		if _, err := topUpQuotaMaxCurrent(order.CreditedQuota); err != nil {
			return err
		}
	} else {
		if plan == nil || plan.Id != order.PlanId || !plan.Enabled || order.CreditedQuota != 0 || walletReceiptAmount != 0 || order.RequestedAmount != 0 {
			return errors.New("invalid alipay subscription order")
		}
		data, err := common.Marshal(plan)
		if err != nil {
			return err
		}
		order.PlanSnapshot = string(data)
	}
	order.Status = common.TopUpStatusPending
	order.CreatedAt = common.GetTimestamp()
	order.Id, order.CompletedAt, order.SubscriptionId, order.TradeNo = 0, 0, 0, nil
	return DB.Transaction(func(tx *gorm.DB) error {
		// Serialize managed merchant changes with new order snapshots. The
		// private-file evaluation path has no settings row and remains separate.
		var configured Option
		lookup := lockForUpdate(tx.Session(&gorm.Session{Logger: dblogger.Default.LogMode(dblogger.Silent)})).Where(map[string]any{"key": AlipaySettingsOption}).First(&configured).Error
		if lookup == nil {
			var merchant pay.Settings
			if err := common.UnmarshalJsonStr(configured.Value, &merchant); err != nil || !merchant.Enabled || merchant.AppID != order.AppId || merchant.SellerID != order.SellerId {
				return errors.New("alipay merchant configuration changed before order creation")
			}
		} else if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		var user User
		if err := lockForUpdate(tx).Select("id", "quota").First(&user, order.UserId).Error; err != nil {
			return err
		}
		if order.PlanId == 0 {
			if user.Quota > common.MaxWalletQuota-order.CreditedQuota {
				return ErrTopUpQuotaLimitExceeded
			}
		} else if plan.MaxPurchasePerUser > 0 {
			var purchased, reserved int64
			if err := tx.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", order.UserId, plan.Id).Count(&purchased).Error; err != nil {
				return err
			}
			if err := tx.Model(&AlipayOrder{}).Where("user_id = ? AND plan_id = ? AND status IN ?", order.UserId, plan.Id, []string{common.TopUpStatusPending, AlipayOrderClosing}).Count(&reserved).Error; err != nil {
				return err
			}
			if purchased+reserved >= int64(plan.MaxPurchasePerUser) {
				return common.NewMessage("Purchase limit for this plan has been reached")
			}
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		money := decimal.NewFromInt(order.AmountCents).Div(decimal.NewFromInt(100)).InexactFloat64()
		if order.PlanId > 0 {
			return tx.Create(&SubscriptionOrder{UserId: order.UserId, PlanId: order.PlanId, Money: money,
				TradeNo: order.OrderId, PaymentMethod: "alipay", PaymentProvider: PaymentProviderAlipay,
				Status: common.TopUpStatusPending, CreateTime: order.CreatedAt}).Error
		}
		return tx.Create(&TopUp{UserId: order.UserId, Amount: walletReceiptAmount, Money: money,
			TradeNo: order.OrderId, PaymentMethod: "alipay", PaymentProvider: PaymentProviderAlipay,
			Status: common.TopUpStatusPending, CreateTime: order.CreatedAt}).Error
	})
}

func GetAlipayOrder(orderID string) (*AlipayOrder, error) {
	var order AlipayOrder
	if err := DB.Where("order_id = ?", orderID).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func GetAlipayOrderByRequest(userID int, requestID string) (*AlipayOrder, error) {
	var order AlipayOrder
	if err := DB.Where("user_id = ? AND request_id = ?", userID, requestID).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func ListRecentAlipayOrders(userID int) ([]AlipayOrder, error) {
	orders := make([]AlipayOrder, 0)
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	err := DB.Where("user_id = ?", userID).Order("id DESC").Limit(20).Find(&orders).Error
	return orders, err
}

// CompleteAlipayOrder is called only with a verified paid gateway result. All
// fulfillment and receipt updates share the snapshot's row lock and transaction.
// A lost HTTP reply can therefore be retried without crediting or granting twice.
func CompleteAlipayOrder(payment pay.Payment, appID, sellerID, callerIP string) (alreadyDone bool, err error) {
	if payment.State != pay.Paid || payment.TradeNo == "" || len(payment.TradeNo) > 64 {
		return false, errors.New("alipay payment is not confirmed")
	}
	var order AlipayOrder
	var quotaAdded int
	var groupChanged bool
	var planTitle string
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("order_id = ?", payment.OrderID).First(&order).Error; err != nil {
			return err
		}
		if order.AppId != appID || order.SellerId != sellerID || order.AmountCents != payment.AmountCents ||
			(order.TradeNo != nil && *order.TradeNo != payment.TradeNo) {
			return errors.New("alipay payment does not match the stored order")
		}
		if order.Status == common.TopUpStatusSuccess {
			alreadyDone = true
			return nil
		}
		if order.Status != common.TopUpStatusPending && order.Status != AlipayOrderClosing {
			return ErrTopUpStatusInvalid
		}
		// Persisting this unique binding rolls back together with all credit.
		if err := tx.Model(&order).Update("trade_no", payment.TradeNo).Error; err != nil {
			return err
		}
		if order.PlanId == 0 {
			if err := creditTopUpQuota(tx, order.UserId, order.CreditedQuota, nil); err != nil {
				return err
			}
			result := tx.Model(&TopUp{}).Where("trade_no = ? AND user_id = ? AND payment_provider = ? AND status = ?",
				order.OrderId, order.UserId, PaymentProviderAlipay, common.TopUpStatusPending).
				Updates(map[string]any{"status": common.TopUpStatusSuccess, "complete_time": common.GetTimestamp()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("alipay native wallet order mismatch")
			}
			quotaAdded = order.CreditedQuota
		} else {
			var plan SubscriptionPlan
			if err := common.UnmarshalJsonStr(order.PlanSnapshot, &plan); err != nil || plan.Id != order.PlanId {
				return errors.New("invalid alipay plan snapshot")
			}
			planTitle = plan.Title
			var user User
			if err := lockForUpdate(tx).Select("id").First(&user, order.UserId).Error; err != nil {
				return err
			}
			subscription, err := CreateUserSubscriptionFromPlanTx(tx, order.UserId, &plan, "order")
			if err != nil {
				return err
			}
			order.SubscriptionId = subscription.Id
			groupChanged = subscription.PrevUserGroup != ""
			var native SubscriptionOrder
			if err := lockForUpdate(tx).Where("trade_no = ? AND user_id = ? AND payment_provider = ? AND status = ?",
				order.OrderId, order.UserId, PaymentProviderAlipay, common.TopUpStatusPending).First(&native).Error; err != nil {
				return err
			}
			if err := upsertSubscriptionTopUpTx(tx, &native); err != nil {
				return err
			}
			// Native receipts expose which provider actually collected payment.
			if err := tx.Model(&TopUp{}).Where("trade_no = ?", order.OrderId).Update("payment_provider", PaymentProviderAlipay).Error; err != nil {
				return err
			}
			if err := tx.Model(&native).Updates(map[string]any{"status": common.TopUpStatusSuccess, "complete_time": common.GetTimestamp()}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&order).Updates(map[string]any{"status": common.TopUpStatusSuccess,
			"completed_at": common.GetTimestamp(), "subscription_id": order.SubscriptionId}).Error
	})
	if err != nil || alreadyDone {
		return alreadyDone, err
	}
	if quotaAdded > 0 {
		syncCreditUserQuotaCache(order.UserId, quotaAdded, "alipay topup")
	}
	if groupChanged {
		refreshSubscriptionUserGroupCache(order.UserId, "alipay subscription purchase")
	}
	amount := fmt.Sprintf("%d.%02d CNY", order.AmountCents/100, order.AmountCents%100)
	message := common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}",
		map[string]any{"provider": "Alipay", "quota": logger.FormatQuota(quotaAdded), "amount": amount})
	if order.PlanId > 0 {
		message = common.NewMessage("Subscription purchased, plan: {{plan}}, payment amount: {{amount}}, payment method: {{method}}",
			map[string]any{"plan": planTitle, "amount": amount, "method": "Alipay"})
	}
	RecordTopupLog(order.UserId, message, callerIP, "alipay", PaymentProviderAlipay)
	return false, nil
}

// BeginAlipayOrderClose persists the customer's cancellation intent before any
// provider request. An uncertain network result stays closing; it cannot issue
// another checkout or release a reserved plan purchase prematurely.
func BeginAlipayOrderClose(orderID string, userID int) (*AlipayOrder, error) {
	var order AlipayOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("order_id = ? AND user_id = ?", orderID, userID).First(&order).Error; err != nil {
			return err
		}
		switch order.Status {
		case common.TopUpStatusSuccess, AlipayOrderClosed, AlipayOrderClosing:
			return nil
		case common.TopUpStatusPending:
			order.Status = AlipayOrderClosing
			return tx.Model(&order).Update("status", AlipayOrderClosing).Error
		default:
			return ErrTopUpStatusInvalid
		}
	})
	return &order, err
}

// CompleteAlipayOrderClose requires a verified closed gateway result. It expires
// the native unpaid receipt in the same transaction. A successful payment is
// never undone here: refund accounting is a different operation.
func CompleteAlipayOrderClose(payment pay.Payment, appID, sellerID string) error {
	if payment.State != pay.Closed || payment.OrderID == "" || len(payment.TradeNo) > 64 {
		return errors.New("alipay closure is not confirmed")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order AlipayOrder
		if err := lockForUpdate(tx).Where("order_id = ?", payment.OrderID).First(&order).Error; err != nil {
			return err
		}
		if order.AppId != appID || order.SellerId != sellerID || order.AmountCents != payment.AmountCents || (order.TradeNo != nil && *order.TradeNo != payment.TradeNo) {
			return errors.New("alipay closure does not match the stored order")
		}
		if order.Status == AlipayOrderClosed {
			return nil
		}
		if order.Status != common.TopUpStatusPending && order.Status != AlipayOrderClosing {
			return ErrTopUpStatusInvalid
		}
		updates := map[string]any{"status": common.TopUpStatusExpired, "complete_time": common.GetTimestamp()}
		var result *gorm.DB
		if order.PlanId == 0 {
			result = tx.Model(&TopUp{}).Where("trade_no = ? AND user_id = ? AND payment_provider = ? AND status = ?", order.OrderId, order.UserId, PaymentProviderAlipay, common.TopUpStatusPending).Updates(updates)
		} else {
			result = tx.Model(&SubscriptionOrder{}).Where("trade_no = ? AND user_id = ? AND payment_provider = ? AND status = ?", order.OrderId, order.UserId, PaymentProviderAlipay, common.TopUpStatusPending).Updates(updates)
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("alipay native unpaid order mismatch")
		}
		return tx.Model(&order).Updates(map[string]any{"status": AlipayOrderClosed, "completed_at": common.GetTimestamp()}).Error
	})
}
