package service

import (
	"errors"
	"io"
	"os"

	"github.com/QuantumNous/new-api/common"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/shopspring/decimal"
)

// AlipayDirectSettings reads Sub2API-compatible credential names from a private
// local file. No credential configuration is exposed through the management API.
// This also keeps local evaluation credentials out of source and option exports.
type AlipayDirectSettings struct {
	pay.Config
	Enabled   bool   `json:"enabled"`
	CNYPerUSD string `json:"cnyPerUsd"`
}

func LoadAlipayDirectSettings() (*AlipayDirectSettings, error) {
	path := os.Getenv("ALIPAY_DIRECT_CONFIG_FILE")
	if path == "" {
		return nil, errors.New("direct Alipay is not configured")
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
