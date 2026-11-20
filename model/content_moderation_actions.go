package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ContentModerationIncident struct {
	ID        int    `gorm:"primaryKey"`
	Key       string `gorm:"column:incident_key;size:64;uniqueIndex;not null"`
	UserID    int    `gorm:"index:idx_moderation_incident_user_created,priority:1;not null"`
	CreatedAt int64  `gorm:"index;index:idx_moderation_incident_user_created,priority:2;not null"`
}

type ContentModerationSuspension struct {
	UserID      int `gorm:"primaryKey;autoIncrement:false"`
	AuditID     int `gorm:"index"`
	AuthVersion int64
	ClearedAt   int64
}

type ModerationActionResult struct {
	Email     string
	Language  string
	Disabled  bool
	NotifyHit bool
	NotifyBan bool
}

var ErrModerationSuspensionChanged = errors.New("the moderation suspension changed")

func ApplyContentModerationIncident(ctx context.Context, event *ContentModerationAudit, hitNotice, banNotice bool, banHits, windowHours int) (ModerationActionResult, error) {
	var action ModerationActionResult
	if event.UserID <= 0 || (event.Decision != ModerationAuditBlock && event.Decision != ModerationAuditHit) || event.Category == "" || event.Mode == "only_record" {
		return action, nil
	}
	if !hitNotice && banHits <= 0 {
		return action, nil
	}
	err := moderationAuditDB(ctx).Transaction(func(tx *gorm.DB) error {
		if event.ID <= 0 {
			var saved ContentModerationAudit
			if err := tx.Select("id").Where("event_key = ?", event.EventKey).Take(&saved).Error; err != nil {
				return err
			}
			event.ID = saved.ID
		}
		var user User
		if err := lockForUpdate(tx).Select("id", "role", "status", "email", "setting").First(&user, event.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return nil
		}
		digest := sha256.Sum256([]byte(strconv.Itoa(user.Id) + "\x00" + event.RequestID))
		incident := ContentModerationIncident{Key: hex.EncodeToString(digest[:]), UserID: user.Id, CreatedAt: event.CreatedAt}
		var duplicate ContentModerationIncident
		result := tx.Where("incident_key = ?", incident.Key).Limit(1).Find(&duplicate)
		if result.Error != nil || result.RowsAffected != 0 {
			return result.Error
		}
		if err := tx.Create(&incident).Error; err != nil {
			return err
		}
		action.Email, action.NotifyHit = user.Email, hitNotice
		action.Language = user.GetSetting().Language
		if banHits > 0 && user.Role == common.RoleCommonUser {
			var suspension ContentModerationSuspension
			if err := tx.Where("user_id = ?", user.Id).Limit(1).Find(&suspension).Error; err != nil {
				return err
			}
			if event.CreatedAt <= suspension.ClearedAt {
				return nil
			}
			since := max(event.CreatedAt-int64(windowHours)*3600, suspension.ClearedAt)
			var hits int64
			if err := tx.Model(&ContentModerationIncident{}).Where("user_id = ? AND created_at > ?", user.Id, since).Count(&hits).Error; err != nil {
				return err
			}
			if hits >= int64(banHits) {
				version, err := IncrementUserAuthVersionWithTx(tx, user.Id)
				if err != nil {
					return err
				}
				if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("status", common.UserStatusDisabled).Error; err != nil {
					return err
				}
				suspension.UserID, suspension.AuditID, suspension.AuthVersion = user.Id, event.ID, version
				if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"audit_id", "auth_version"})}).Create(&suspension).Error; err != nil {
					return err
				}
				action.Disabled, action.NotifyBan = true, banNotice
			}
		}
		fields := map[string]any{"auto_disabled": action.Disabled}
		if action.NotifyHit {
			fields["hit_email"] = "queued"
		}
		if action.NotifyBan {
			fields["ban_email"] = "queued"
		}
		return tx.Model(&ContentModerationAudit{}).Where("id = ?", event.ID).Updates(fields).Error
	})
	if err != nil {
		return ModerationActionResult{}, err
	}
	return action, nil
}

func SetContentModerationMailStatus(ctx context.Context, auditID int, suspension bool, outcome string) error {
	column := "hit_email"
	if suspension {
		column = "ban_email"
	}
	return moderationAuditDB(ctx).Model(&ContentModerationAudit{}).Where("id = ?", auditID).Update(column, outcome).Error
}

func ReleaseContentModerationSuspension(ctx context.Context, userID, auditID int) error {
	return moderationAuditDB(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id", "role", "status", "auth_version").First(&user, userID).Error; err != nil {
			return err
		}
		var suspension ContentModerationSuspension
		result := tx.Where("user_id = ? AND audit_id = ?", userID, auditID).Limit(1).Find(&suspension)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 || suspension.AuthVersion <= 0 || user.AuthVersion != suspension.AuthVersion || user.Status != common.UserStatusDisabled || user.Role != common.RoleCommonUser {
			return ErrModerationSuspensionChanged
		}
		if _, err := IncrementUserAuthVersionWithTx(tx, userID); err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("status", common.UserStatusEnabled).Error; err != nil {
			return err
		}
		return clearModerationSuspension(tx, suspension, time.Now().Unix())
	})
}

func clearModerationSuspension(tx *gorm.DB, suspension ContentModerationSuspension, now int64) error {
	if err := tx.Model(&ContentModerationSuspension{}).Where("user_id = ?", suspension.UserID).Updates(map[string]any{"auth_version": 0, "cleared_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&ContentModerationAudit{}).Where("id = ?", suspension.AuditID).Update("released_at", now).Error
}

func SetUserStatusManually(userID, status, actorRole int) (bool, error) {
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id", "role", "status").First(&user, userID).Error; err != nil {
			return err
		}
		if (actorRole != common.RoleRootUser && actorRole <= user.Role) || (user.Role == common.RoleRootUser && status == common.UserStatusDisabled) {
			return ErrModerationSuspensionChanged
		}
		if status != user.Status {
			if _, err := IncrementUserAuthVersionWithTx(tx, userID); err != nil {
				return err
			}
			if err := tx.Model(&User{}).Where("id = ?", userID).Update("status", status).Error; err != nil {
				return err
			}
			changed = true
		}
		var suspension ContentModerationSuspension
		result := tx.Where("user_id = ? AND auth_version > 0", userID).Limit(1).Find(&suspension)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		return clearModerationSuspension(tx, suspension, time.Now().Unix())
	})
	return changed && err == nil, err
}

func DeleteExpiredModerationIncidents(ctx context.Context, now int64) error {
	database := moderationAuditDB(ctx)
	cutoff := now - 90*86400
	var ids []int
	if err := database.Model(&ContentModerationIncident{}).Where("created_at <= ?", cutoff).Order("created_at ASC").Limit(500).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return err
	}
	return database.Where("id IN ? AND created_at <= ?", ids, cutoff).Delete(&ContentModerationIncident{}).Error
}
