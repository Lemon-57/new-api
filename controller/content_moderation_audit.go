package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type moderationAuditQuery struct {
	RequestID  string `form:"request_id" binding:"max=128"`
	InstanceID string `form:"instance_id" binding:"max=128"`
	Stage      string `form:"stage" binding:"omitempty,oneof=ingress reply"`
	Decision   string `form:"decision" binding:"max=24"`
	UserID     int    `form:"user_id" binding:"min=0"`
	BeforeID   int    `form:"before_id" binding:"min=0"`
	Start      int64  `form:"start" binding:"min=0"`
	End        int64  `form:"end" binding:"min=0"`
	Limit      int    `form:"limit" binding:"omitempty,min=1,max=100"`
}

func ListContentModerationAudits(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var query moderationAuditQuery
	if err := c.ShouldBindQuery(&query); err != nil || !model.ValidContentModerationDecision(query.Decision) || (query.End > 0 && query.Start > query.End) {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	if query.Limit == 0 {
		query.Limit = 25
	}
	rows, more, err := model.ListContentModerationAudits(c.Request.Context(), model.ContentModerationAuditFilter{
		RequestID: query.RequestID, InstanceID: query.InstanceID, Stage: query.Stage, Decision: query.Decision,
		UserID: query.UserID, BeforeID: query.BeforeID, Start: query.Start, End: query.End, Limit: query.Limit,
	})
	if err != nil {
		logger.LogWarn(c, common.LogText("failed to read content moderation audits"))
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Please try again later."))
		return
	}
	next := 0
	if more {
		next = rows[len(rows)-1].ID
	}
	common.ApiSuccess(c, gin.H{"items": rows, "next_before_id": next})
}

func ContentModerationStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	loaded, _ := jsplugin.DefaultRegistry.GetIgnoringMasterSwitch("content-moderation")
	version := ""
	if loaded != nil {
		version = loaded.Meta.Version
	}
	instances := make([]gin.H, 0)
	for _, instance := range request_plugin_setting.GetInstances() {
		if instance.Plugin == "content-moderation" {
			engine, keys := service.ModerationKeyHealth(instance.Config)
			instances = append(instances, gin.H{"instance_id": instance.ID, "name": instance.Name, "engine": engine, "keys": keys})
		}
	}
	common.ApiSuccess(c, gin.H{"node": common.NodeName, "version": version, "instances": instances, "queues": service.ModerationQueueStatuses()})
}

func ReleaseContentModerationAuditSuspension(c *gin.Context) {
	auditID, parseErr := strconv.Atoi(c.Param("id"))
	if parseErr != nil || auditID <= 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	event, err := model.ContentModerationSuspensionAudit(c.Request.Context(), auditID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Please try again later."))
		return
	}
	if !event.AutoDisabled || event.ReleasedAt > 0 {
		common.ApiErrorStatus(c, http.StatusConflict, common.NewMessage("The moderation suspension changed"))
		return
	}
	proof := requireAdminUserProof(c, service.VerificationScopeAdminUserManage, service.AdminUserManageContext{UserID: event.UserID, Action: "enable"})
	if proof == nil {
		return
	}
	if err := model.ReleaseContentModerationSuspension(c.Request.Context(), event.UserID, auditID); err != nil {
		if errors.Is(err, model.ErrModerationSuspensionChanged) || errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorStatus(c, http.StatusConflict, common.NewMessage("The moderation suspension changed"))
		} else {
			common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Please try again later."))
		}
		return
	}
	if err := service.RefreshAfterModerationStatusChange(event.UserID, "content_moderation_released"); err != nil {
		logger.LogWarn(c, common.LogText("failed to publish a content moderation account status"))
	}
	recordManageAuditFor(c, event.UserID, "user.manage", map[string]any{"action": "enable", "moderation_audit_id": auditID, "verification_method": proof.Method})
	common.ApiSuccess(c, nil)
}
