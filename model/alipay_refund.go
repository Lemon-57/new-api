package model

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	AlipayRefundPending = "pending"
	AlipayRefunded      = "refunded"
	AlipayRefundFailed  = "failed"
)

var (
	ErrAlipayRefundNotAllowed = errors.New("alipay refund is not allowed for this order")
	ErrAlipayRefundMismatch   = errors.New("alipay refund request does not match the stored refund")
	ErrAlipayRefundState      = errors.New("alipay refund state transition is invalid")
)

// AlipayRefundRecord is the durable handoff between local refund authorization
// and the non-idempotent provider call. The request number is written before the
// call, so a timeout or process restart can only retry the same refund request.
type AlipayRefundRecord struct {
	Id              int    `json:"id"`
	OrderId         string `json:"order_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	RefundRequestId string `json:"refund_request_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	TradeNo         string `json:"trade_no" gorm:"type:varchar(64);not null"`
	AmountCents     int64  `json:"amount_cents" gorm:"not null"`
	Reason          string `json:"reason" gorm:"type:varchar(255)"`
	Status          string `json:"status" gorm:"type:varchar(32);not null;index"`
	ProviderPayload string `json:"-" gorm:"type:text"`
	RequestedAt     int64  `json:"requested_at" gorm:"not null"`
	CompletedAt     int64  `json:"completed_at"`
}

// PrepareAlipayRefund durably reserves a stable refund request number. Repeating
// the same order/amount/reason returns the existing record and never creates a
// second provider request. No provider call happens inside this transaction.
func PrepareAlipayRefund(orderID string, amountCents int64, reason string) (*AlipayRefundRecord, pay.Refund, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" || amountCents <= 0 || strings.TrimSpace(reason) == "" {
		return nil, pay.Refund{}, ErrAlipayRefundNotAllowed
	}
	var record AlipayRefundRecord
	var source AlipayOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		var order AlipayOrder
		if err := lockForUpdate(tx).Where("order_id = ?", orderID).First(&order).Error; err != nil {
			return err
		}
		if order.Status != common.TopUpStatusSuccess || order.TradeNo == nil || amountCents > order.AmountCents {
			return ErrAlipayRefundNotAllowed
		}
		source = order
		var existing AlipayRefundRecord
		findErr := lockForUpdate(tx).Where("order_id = ?", orderID).First(&existing).Error
		if findErr == nil {
			if existing.AmountCents != amountCents || existing.Reason != reason || existing.TradeNo != *order.TradeNo {
				return ErrAlipayRefundMismatch
			}
			record = existing
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		record = AlipayRefundRecord{OrderId: orderID, RefundRequestId: "NARF" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			TradeNo: *order.TradeNo, AmountCents: amountCents, Reason: reason, Status: AlipayRefundPending, RequestedAt: common.GetTimestamp()}
		return tx.Create(&record).Error
	})
	if err != nil {
		return nil, pay.Refund{}, err
	}
	return &record, record.ToProviderRefund(source), nil
}

func SetAlipayRefundState(requestID, state, providerPayload string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || (state != AlipayRefunded && state != AlipayRefundFailed) {
		return ErrAlipayRefundState
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var record AlipayRefundRecord
		if err := lockForUpdate(tx).Where("refund_request_id = ?", requestID).First(&record).Error; err != nil {
			return err
		}
		if record.Status == state {
			return nil
		}
		if record.Status != AlipayRefundPending {
			return ErrAlipayRefundState
		}
		record.Status = state
		record.CompletedAt = common.GetTimestamp()
		if providerPayload != "" {
			record.ProviderPayload = providerPayload
		}
		return tx.Save(&record).Error
	})
}

func GetAlipayRefundByOrder(orderID string) (*AlipayRefundRecord, error) {
	var record AlipayRefundRecord
	if err := DB.Where("order_id = ?", strings.TrimSpace(orderID)).First(&record).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

func (r AlipayRefundRecord) ToProviderRefund(order AlipayOrder) pay.Refund {
	return pay.Refund{Order: pay.Order{OrderID: r.OrderId, TradeNo: r.TradeNo, AmountCents: order.AmountCents},
		RefundRequestID: r.RefundRequestId, RefundCents: r.AmountCents, Reason: r.Reason,
		RequestedAt: time.Unix(r.RequestedAt, 0)}
}
