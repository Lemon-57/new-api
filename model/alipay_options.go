package model

import (
	"errors"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const AlipayVerifierRegistryOption = "AlipayVerifierRegistry"

var directAlipayOptionKeys = []string{
	"AlipayAppID", "AlipayPrivateKey", "AlipayPublicKey", "AlipayNotifyURL", "AlipayReturnURL", AlipayVerifierRegistryOption,
}

var ErrAlipayOptionsChanged = errors.New("Alipay settings changed concurrently")

func IsDirectAlipayOption(key string) bool {
	return slices.Contains(directAlipayOptionKeys, key)
}

func loadDirectAlipayOptions(query *gorm.DB) (map[string]string, error) {
	var rows []Option
	if err := query.Where(map[string]any{"key": directAlipayOptionKeys}).Find(&rows).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(directAlipayOptionKeys))
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	return values, nil
}

func GetDirectAlipayOptions() (map[string]string, error) {
	return loadDirectAlipayOptions(DB)
}

func ReplaceDirectAlipayOptions(previous, next map[string]string) error {
	err := DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		anchor := Option{Key: AlipayVerifierRegistryOption}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&anchor).Error; err != nil {
			return err
		}
		current, err := loadDirectAlipayOptions(lockForUpdate(tx))
		if err != nil {
			return err
		}
		rows := make([]Option, 0, len(directAlipayOptionKeys))
		for _, key := range directAlipayOptionKeys {
			if current[key] != previous[key] {
				return ErrAlipayOptionsChanged
			}
			rows = append(rows, Option{Key: key, Value: next[key]})
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"}),
		}).Create(&rows).Error
	})
	if err != nil {
		return err
	}
	BumpSyncTopic(SyncTopicOptions)
	return nil
}
