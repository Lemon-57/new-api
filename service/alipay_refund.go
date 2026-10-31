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
	if err != nil {
		return nil, model.AlipayRefundPending, errors.New("direct Alipay is not configured")
	}
	order, err := model.GetAlipayOrder(orderID)
	if err != nil {
		return nil, model.AlipayRefundPending, err
	}
	if order.AppId != settings.AppID || order.SellerId != settings.SellerID {
		return nil, model.AlipayRefundPending, model.ErrAlipayRefundMismatch
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
	if state == model.AlipayRefunded || state == model.AlipayRefundFailed {
		if err := model.SetAlipayRefundState(record.RefundRequestId, state, ""); err != nil {
			return record, model.AlipayRefundPending, err
		}
	}
	record.Status = state
	_ = callerIP // retained for the future audit record; no secret is logged here.
	return record, state, nil
}

func ReconcileAlipayRefund(ctx context.Context, orderID, callerIP string) (*model.AlipayRefundRecord, string, error) {
	client, settings, err := GetAlipayDirectClient()
	if err != nil {
		return nil, model.AlipayRefundPending, errors.New("direct Alipay is not configured")
	}
	order, err := model.GetAlipayOrder(orderID)
	if err != nil {
		return nil, model.AlipayRefundPending, err
	}
	if order.TradeNo == nil || order.AppId != settings.AppID || order.SellerId != settings.SellerID {
		return nil, model.AlipayRefundPending, model.ErrAlipayRefundMismatch
	}
	record, err := model.GetAlipayRefundByOrder(orderID)
	if err != nil {
		return nil, model.AlipayRefundPending, err
	}
	if record.Status != model.AlipayRefundPending {
		return record, record.Status, nil
	}
	refundRequest := record.ToProviderRefund(*order)
	state, queryErr := client.QueryRefund(ctx, refundRequest, time.Now())
	if queryErr != nil {
		return record, state, queryErr
	}
	record.Status = state
	if state == model.AlipayRefunded || state == model.AlipayRefundFailed {
		if err := model.SetAlipayRefundState(record.RefundRequestId, state, ""); err != nil {
			return record, model.AlipayRefundPending, err
		}
	}
	_ = callerIP
	return record, state, nil
}
