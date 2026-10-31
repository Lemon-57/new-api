package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
)

// ReconcileAlipayOrder queries the current gateway state. A durable closing
// marker additionally authorizes retrying the original close request. It never
// creates another checkout, even if a prior close reply was lost.
func ReconcileAlipayOrder(ctx context.Context, order *model.AlipayOrder, callerIP string) (*model.AlipayOrder, error) {
	if order.Status == common.TopUpStatusSuccess || order.Status == model.AlipayOrderClosed {
		return model.GetAlipayOrder(order.OrderId)
	}
	client, settings, err := GetAlipayDirectClient()
	if err != nil {
		return nil, err
	}
	if order.AppId != settings.AppID || order.SellerId != settings.SellerID {
		return nil, errors.New("alipay merchant does not match the stored order")
	}
	expected := pay.Order{OrderID: order.OrderId, AmountCents: order.AmountCents}
	if order.TradeNo != nil {
		expected.TradeNo = *order.TradeNo
	}
	if order.Status == model.AlipayOrderClosing {
		// Close may return an error when payment won the race, or after a lost
		// reply. Only the following signed query decides the durable outcome.
		_ = client.Close(ctx, order.OrderId)
	}
	payment, err := client.Query(ctx, expected)
	if err != nil {
		return nil, err
	}
	switch payment.State {
	case pay.Paid:
		if _, err := model.CompleteAlipayOrder(*payment, settings.AppID, settings.SellerID, callerIP); err != nil {
			return nil, err
		}
	case pay.Closed:
		if err := model.CompleteAlipayOrderClose(*payment, settings.AppID, settings.SellerID); err != nil {
			return nil, err
		}
	case pay.Pending:
		// A not-found response is also pending in the provider adapter. Do not
		// release an order whose original create request may still be in flight.
	default:
		return nil, errors.New("unknown alipay reconciliation state")
	}
	return model.GetAlipayOrder(order.OrderId)
}
