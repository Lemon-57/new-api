package model

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	pay "github.com/QuantumNous/new-api/pkg/payment/alipay"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A single option keeps the entire merchant configuration atomic, including
// across instances. Payment readers use the committed row, never partially
// refreshed in-memory fields. The Secret suffix excludes it from GetOptions.
const AlipaySettingsOption = "AlipayDirectConfigSecret"

func LoadManagedAlipaySettings() (*pay.Settings, error) {
	if DB == nil {
		return nil, errors.New("direct Alipay is not configured")
	}
	var option Option
	if err := DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Where(map[string]any{"key": AlipaySettingsOption}).First(&option).Error; err != nil {
		return nil, errors.New("direct Alipay is not configured")
	}
	var settings pay.Settings
	if err := common.UnmarshalJsonStr(option.Value, &settings); err != nil {
		return nil, errors.New("direct Alipay configuration is invalid")
	}
	return &settings, nil
}

// SaveManagedAlipaySettings keeps blank key inputs unchanged and validates the
// complete proposed configuration before a single transactional write. Drafts
// can be saved disabled, but enabling requires valid keys and a callback URL.
func SaveManagedAlipaySettings(proposed pay.Settings) (*pay.Settings, error) {
	var saved pay.Settings
	err := DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var row Option
		if err := tx.Where(map[string]any{"key": AlipaySettingsOption}).Attrs(Option{Value: "{}"}).FirstOrCreate(&row).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where(map[string]any{"key": AlipaySettingsOption}).First(&row).Error; err != nil {
			return err
		}
		var previous pay.Settings
		if err := common.UnmarshalJsonStr(row.Value, &previous); err != nil {
			return errors.New("stored Alipay configuration cannot be read")
		}
		proposed.AppID = strings.TrimSpace(proposed.AppID)
		proposed.SellerID = strings.TrimSpace(proposed.SellerID)
		proposed.PrivateKey = strings.TrimSpace(proposed.PrivateKey)
		proposed.PublicKey = strings.TrimSpace(proposed.PublicKey)
		proposed.NotifyURL = strings.TrimSpace(proposed.NotifyURL)
		proposed.ReturnURL = strings.TrimSpace(proposed.ReturnURL)
		proposed.CNYPerUSD = strings.TrimSpace(proposed.CNYPerUSD)
		if proposed.PrivateKey == "" {
			proposed.PrivateKey = previous.PrivateKey
		}
		if proposed.PublicKey == "" {
			proposed.PublicKey = previous.PublicKey
		}
		// Only fields supported by this management form are persisted.
		proposed.AlipayPublicKey, proposed.PaymentMode = "", ""
		if proposed.CNYPerUSD != "" {
			rate, err := decimal.NewFromString(proposed.CNYPerUSD)
			if err != nil || !rate.IsPositive() || rate.GreaterThan(decimal.NewFromInt(1000000)) {
				return errors.New("invalid Alipay CNY per USD rate")
			}
		}
		for _, raw := range []string{proposed.NotifyURL, proposed.ReturnURL} {
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
				return errors.New("invalid Alipay callback URL")
			}
			loopback := u.Hostname() == "localhost"
			if ip := net.ParseIP(u.Hostname()); ip != nil {
				loopback = ip.IsLoopback()
			}
			if u.Scheme != "https" && !(proposed.Sandbox && loopback && u.Scheme == "http") {
				return errors.New("Alipay callback URL requires HTTPS")
			}
		}
		if proposed.Enabled {
			if proposed.NotifyURL == "" {
				return errors.New("Alipay notification URL is required")
			}
			if _, err := pay.New(proposed.Config); err != nil {
				return errors.New("Alipay application and verification keys must be valid before enabling")
			}
		}
		if previous.AppID != "" && (previous.AppID != proposed.AppID || previous.SellerID != proposed.SellerID || previous.Sandbox != proposed.Sandbox || previous.PublicKey != proposed.PublicKey) {
			var orders int64
			if err := tx.Model(&AlipayOrder{}).Count(&orders).Error; err != nil {
				return err
			}
			if orders > 0 {
				return errors.New("Alipay merchant or verification key changes require reconciliation of existing orders")
			}
		}
		data, err := common.Marshal(proposed)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Update("value", string(data)).Error; err != nil {
			return err
		}
		saved = proposed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}
