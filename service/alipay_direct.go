package service

import (
	"errors"
	"io"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/shopspring/decimal"
)

// AlipayDirectSettings preserves Sub2API-compatible credential names. An explicit
// private file overrides the managed option; management reads never return keys.
type AlipayDirectSettings = pay.Settings

func LoadAlipayDirectSettings() (*AlipayDirectSettings, error) {
	path := os.Getenv("ALIPAY_DIRECT_CONFIG_FILE")
	if path == "" {
		return model.LoadManagedAlipaySettings()
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read direct Alipay configuration")
	}
	defer file.Close()
	var settings AlipayDirectSettings
	if err := common.DecodeJson(io.LimitReader(file, 1024*1024), &settings); err != nil {
		return nil, errors.New("direct Alipay configuration is invalid")
	}
	return &settings, nil
}

func GetAlipayDirectClient() (*pay.Client, *AlipayDirectSettings, error) {
	settings, err := LoadAlipayDirectSettings()
	if err != nil {
		return nil, nil, err
	}
	client, err := pay.New(settings.Config)
	if err != nil {
		return nil, nil, err
	}
	return client, settings, nil
}

// AlipayPlanAmount snapshots the exact CNY total before checkout. USD plans
// require an explicit conversion rate; display currency settings do not silently
// reinterpret a plan's stored currency or its price.
func AlipayPlanAmount(price decimal.Decimal, currency string, settings *AlipayDirectSettings) (int64, error) {
	switch currency {
	case "CNY":
	case "USD":
		rate, err := decimal.NewFromString(settings.CNYPerUSD)
		if err != nil || !rate.IsPositive() {
			return 0, errors.New("direct Alipay requires an explicit CNY per USD rate for USD plans")
		}
		price = price.Mul(rate)
	default:
		return 0, errors.New("unsupported direct Alipay plan currency")
	}
	return pay.ParseAmount(price.Round(2).StringFixed(2))
}
