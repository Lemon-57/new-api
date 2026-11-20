package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"html"
	"net/mail"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
)

var moderationNotifications = newModerationQueue()

func applyContentModerationActions(ctx context.Context, event *model.ContentModerationAudit, options moderationAuditOptions, settingsHash [32]byte) {
	if event.Mode == "only_record" || (event.Decision != model.ModerationAuditBlock && event.Decision != model.ModerationAuditHit) ||
		(event.Category == "" || event.FailureClass != "") || (!options.NotifyHit && options.BanHits <= 0) {
		return
	}
	current := false
	for _, instance := range request_plugin_setting.GetInstances() {
		if instance.ID == event.InstanceID && instance.Enabled && instance.Plugin == "content-moderation" && sha256.Sum256(instance.Config) == settingsHash {
			current = true
			break
		}
	}
	if !current {
		return
	}
	window := options.BanHours
	if window <= 0 {
		window = 720
	}
	actions, err := model.ApplyContentModerationIncident(ctx, event, options.NotifyHit, options.NotifyBan, min(options.BanHits, 1000), min(window, 2160))
	if err != nil {
		logger.LogWarn(ctx, common.LogText("failed to apply a content moderation action"))
		return
	}
	if actions.Disabled {
		if err := RefreshAfterModerationStatusChange(event.UserID, "content_moderation_disabled"); err != nil {
			logger.LogWarn(ctx, common.LogText("failed to publish a content moderation account status"))
		}
	}
	if !actions.NotifyHit && !actions.NotifyBan {
		return
	}
	auditID, instanceID, requestID := event.ID, event.InstanceID, event.RequestID
	if !moderationNotifications.enqueue(moderationJob{instance: instanceID, workers: 2, bytes: 1024, run: func(ctx context.Context) {
		if actions.NotifyHit {
			deliverModerationNotice(ctx, auditID, requestID, actions.Email, actions.Language, false)
		}
		if actions.NotifyBan {
			deliverModerationNotice(ctx, auditID, requestID, actions.Email, actions.Language, true)
		}
	}}, 256) {
		if actions.NotifyHit {
			_ = model.SetContentModerationMailStatus(ctx, auditID, false, "not_sent")
		}
		if actions.NotifyBan {
			_ = model.SetContentModerationMailStatus(ctx, auditID, true, "not_sent")
		}
	}
}

func deliverModerationNotice(ctx context.Context, auditID int, requestID, email, language string, suspension bool) {
	outcome := "not_sent"
	recipient := strings.TrimSpace(email)
	address, err := mail.ParseAddress(recipient)
	if err == nil && address.Address == recipient && !strings.ContainsAny(recipient, "\r\n;") {
		subject, notice := i18n.MsgModerationHitSubject, i18n.MsgModerationHitBody
		if suspension {
			subject, notice = i18n.MsgModerationBanSubject, i18n.MsgModerationBanBody
		}
		args := map[string]any{"RequestID": requestID}
		body := "<p>" + html.EscapeString(i18n.Translate(language, notice, args)) + "</p>"
		send, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := common.SendEmailContext(send, i18n.Translate(language, subject), recipient, body)
		cancel()
		outcome = "accepted"
		if err != nil {
			outcome = "failed"
		}
	}
	if err := model.SetContentModerationMailStatus(ctx, auditID, suspension, outcome); err != nil {
		logger.LogWarn(ctx, common.LogText("failed to store content moderation email status"))
	}
}

func RefreshAfterModerationStatusChange(userID int, reason string) error {
	cacheErr := model.PublishUserAuthCache(userID)
	tokenErr := model.InvalidateUserTokensCache(userID)
	_, sessionErr := model.RevokeAllUserSessions(userID, reason)
	return errors.Join(cacheErr, tokenErr, sessionErr)
}
