package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// RequestAlipayRefund performs the durable handoff, then calls the provider
// with the already-persisted request number. An uncertain result deliberately
// leaves the record pending for ReconcileAlipayRefund.
func RequestAlipayRefund(ctx context.Context, orderID string, amountCents int64, reason, callerIP string) (*model.AlipayRefundRecord, string, error) {
	client, settings, err := GetAlipayDirectClient()
	if err != nil || !settings.Enabled {
		return nil, model.AlipayRefundPending, errors.New("direct Alipay is not configured")
	}
	record, refundRequest, err := model.PrepareAlipayRefund(orderID, amountCents, reason)
	if err != nil {
		return nil, model.AlipayRefundPending, err
	}
	if record.Status != model.AlipayRefundPending {
		return record, record.Status, nil
	}
	state, providerErr := client.Refund(ctx, refundRequest)
	if providerErr != nil {
		return record, state, providerErr
	}
	if err := model.SetAlipayRefundState(record.RefundRequestId, state, ""); err != nil {
		return record, model.AlipayRefundPending, err
	}
	_ = callerIP // retained for the future audit record; no secret is logged here.
	return record, state, nil
}

func ReconcileAlipayRefund(ctx context.Context, orderID, callerIP string) (*model.AlipayRefundRecord, string, error) {
	client, settings, err := GetAlipayDirectClient()
	if err != nil || !settings.Enabled {
		return nil, model.AlipayRefundPending, errors.New("direct Alipay is not configured")
	}
	order, err := model.GetAlipayOrder(orderID)
	if err != nil || order.TradeNo == nil {
		return nil, model.AlipayRefundPending, err
	}
	record, refundRequest, err := model.PrepareAlipayRefund(orderID, 0, "")
	if err != nil {
		// Prepare requires the amount/reason for a new record. Existing records
		// are loaded directly to make reconciliation safe after process restart.
		record, err = model.GetAlipayRefundByOrder(orderID)
		if err != nil {
			return nil, model.AlipayRefundPending, err
		}
		refundRequest = record.ToProviderRefund()
	}
	state, queryErr := client.QueryRefund(ctx, refundRequest, time.Unix(record.RequestedAt, 0))
	if queryErr != nil {
		return record, state, queryErr
	}
	if state == model.AlipayRefunded || state == model.AlipayRefundFailed {
		if err := model.SetAlipayRefundState(record.RefundRequestId, state, ""); err != nil {
			return record, model.AlipayRefundPending, err
		}
	}
	_ = callerIP
	return record, state, nil
}
