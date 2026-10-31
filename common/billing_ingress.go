package common

import "context"

// BillingIngress is authenticated by the serving edge, never by forwarded IP
// or the caller's Host. Copies remain immutable throughout a request/WS turn.
type BillingIngress struct {
	Name            string   `json:"name"`
	GroupMultiplier float64  `json:"group_multiplier"`
	ImageMultiplier float64  `json:"image_multiplier"`
	ImageModels     []string `json:"image_models,omitempty"`
}

type billingIngressKey struct{}

func WithBillingIngress(ctx context.Context, ingress BillingIngress) context.Context {
	ingress.ImageModels = append([]string(nil), ingress.ImageModels...)
	return context.WithValue(ctx, billingIngressKey{}, ingress)
}

func BillingIngressFromContext(ctx context.Context) (BillingIngress, bool) {
	if ctx == nil {
		return BillingIngress{}, false
	}
	value, ok := ctx.Value(billingIngressKey{}).(BillingIngress)
	value.ImageModels = append([]string(nil), value.ImageModels...)
	return value, ok
}
