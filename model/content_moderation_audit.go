package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

const (
	ModerationAuditBlock   = "blocked"
	ModerationAuditHit     = "observed_hit"
	ModerationAuditHistory = "hash_block"
	ModerationAuditPass    = "allowed"
	ModerationAuditFailure = "error"
	ModerationAuditSkipped = "skipped"
)

type ContentModerationAudit struct {
	ID           int    `json:"id" gorm:"primaryKey"`
	EventKey     string `json:"-" gorm:"size:64;uniqueIndex;not null"`
	RequestID    string `json:"request_id" gorm:"size:128;index;not null"`
	UserID       int    `json:"user_id" gorm:"index:idx_content_audit_user_created,priority:1;not null"`
	InstanceID   string `json:"instance_id" gorm:"size:128;index;not null"`
	InstanceName string `json:"instance_name" gorm:"size:128;not null"`
	Model        string `json:"model" gorm:"size:128"`
	Engine       string `json:"engine" gorm:"size:16;not null"`
	EngineModel  string `json:"engine_model" gorm:"size:128"`
	Stage        string `json:"stage" gorm:"size:16;not null"`
	Mode         string `json:"mode" gorm:"size:16;not null"`
	Decision     string `json:"decision" gorm:"size:24;index;not null"`
	Category     string `json:"category,omitempty" gorm:"size:128"`
	Scores       string `json:"scores,omitempty" gorm:"type:text"`
	Thresholds   string `json:"thresholds,omitempty" gorm:"type:text"`
	InputExcerpt string `json:"input_excerpt,omitempty" gorm:"type:text"`
	FailureClass string `json:"failure_class,omitempty" gorm:"size:32"`
	HitEmail     string `json:"hit_email,omitempty" gorm:"size:16"`
	BanEmail     string `json:"ban_email,omitempty" gorm:"size:16"`
	AutoDisabled bool   `json:"auto_disabled"`
	ReleasedAt   int64  `json:"released_at,omitempty"`
	RiskHash     string `json:"-" gorm:"size:64;index:idx_content_audit_hash_expires,priority:1"`
	CreatedAt    int64  `json:"created_at" gorm:"index:idx_content_audit_user_created,priority:2;not null"`
	ExpiresAt    int64  `json:"expires_at" gorm:"index;index:idx_content_audit_hash_expires,priority:2;not null"`
}

type ContentModerationAuditFilter struct {
	RequestID  string
	InstanceID string
	Stage      string
	Decision   string
	UserID     int
	BeforeID   int
	Start      int64
	End        int64
	Limit      int
}

func moderationAuditDB(ctx context.Context) *gorm.DB {
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
}

func StoreContentModerationAudit(ctx context.Context, event *ContentModerationAudit) error {
	return moderationAuditDB(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_key"}}, DoNothing: true}).Create(event).Error
}

func ContentModerationHistory(ctx context.Context, fingerprint string, now int64) (string, bool, error) {
	var event ContentModerationAudit
	result := moderationAuditDB(ctx).Select("category").Where("risk_hash = ? AND expires_at > ? AND decision IN ?", fingerprint, now,
		[]string{ModerationAuditBlock, ModerationAuditHit}).Order("id DESC").Limit(1).Find(&event)
	return event.Category, result.RowsAffected == 1, result.Error
}

func ListContentModerationAudits(ctx context.Context, filter ContentModerationAuditFilter) ([]ContentModerationAudit, bool, error) {
	query := moderationAuditDB(ctx).Model(&ContentModerationAudit{}).Where("expires_at > ?", time.Now().Unix())
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	if filter.InstanceID != "" {
		query = query.Where("instance_id = ?", filter.InstanceID)
	}
	if filter.Stage != "" {
		query = query.Where("stage = ?", filter.Stage)
	}
	if filter.Decision != "" {
		query = query.Where("decision = ?", filter.Decision)
	}
	if filter.UserID > 0 {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.BeforeID > 0 {
		query = query.Where("id < ?", filter.BeforeID)
	}
	if filter.Start > 0 {
		query = query.Where("created_at >= ?", filter.Start)
	}
	if filter.End > 0 {
		query = query.Where("created_at <= ?", filter.End)
	}
	limit := max(1, min(filter.Limit, 100))
	rows := make([]ContentModerationAudit, 0, limit+1)
	if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	return rows, more, nil
}

func ContentModerationSuspensionAudit(ctx context.Context, auditID int) (ContentModerationAudit, error) {
	var event ContentModerationAudit
	err := moderationAuditDB(ctx).Select("id", "user_id", "auto_disabled", "released_at").First(&event, auditID).Error
	return event, err
}

func DeleteExpiredContentModerationAudits(ctx context.Context, now int64, batch int) (int64, error) {
	database := moderationAuditDB(ctx)
	var ids []int
	if err := database.Model(&ContentModerationAudit{}).Where("expires_at <= ?", now).Order("expires_at ASC").Limit(max(1, min(batch, 1000))).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return 0, err
	}
	result := database.Where("id IN ? AND expires_at <= ?", ids, now).Delete(&ContentModerationAudit{})
	return result.RowsAffected, result.Error
}

func ValidContentModerationDecision(decision string) bool {
	switch strings.TrimSpace(decision) {
	case "", ModerationAuditBlock, ModerationAuditHit, ModerationAuditHistory, ModerationAuditPass, ModerationAuditFailure, ModerationAuditSkipped:
		return true
	default:
		return false
	}
}
