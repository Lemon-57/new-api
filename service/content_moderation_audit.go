package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/tidwall/gjson"
)

const ModerationObservationMaxBytes = 256 << 10

const moderationAuditCategoryLimit = 13

type moderationAuditOptions struct {
	Engine        string `json:"engine"`
	Model         string `json:"model"`
	TypeSafeModel string `json:"typeSafeModel"`
	Mode          string `json:"auditMode"`
	Sample        *int   `json:"auditSampleRate"`
	Workers       int    `json:"auditWorkers"`
	QueueLimit    int    `json:"auditQueueLimit"`
	RecordPass    bool   `json:"auditRecordNonHits"`
	KeepExcerpt   bool   `json:"auditStoreExcerpt"`
	HashPrecheck  bool   `json:"auditHashPrecheck"`
	HitDays       int    `json:"auditHitRetentionDays"`
	NonHitDays    int    `json:"auditNonHitRetentionDays"`
	OnlyRecord    []struct {
		UserID int `json:"userId"`
	} `json:"auditOnlyRecordUsers"`
	NotifyHit bool `json:"auditEmailOnHit"`
	NotifyBan bool `json:"auditEmailOnBan"`
	BanHits   int  `json:"auditBanHits"`
	BanHours  int  `json:"auditBanWindowHours"`
}

type ModerationAudit struct {
	options      moderationAuditOptions
	event        model.ContentModerationAudit
	categories   map[string]bool
	thresholds   map[string]float64
	scores       map[string]float64
	policyKey    string
	observed     bool
	sampled      bool
	stage        string
	fetchCount   int
	historyHit   bool
	settingsHash [32]byte
	eventScope   string
}

type moderationEventScopeKey struct{}

func WithModerationEventScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, moderationEventScopeKey{}, scope)
}

func NewModerationAudit(ctx context.Context, plugin *jsplugin.LoadedPlugin, instance *request_plugin_setting.Instance, stage, modelName string, userID int) *ModerationAudit {
	if userID <= 0 || (stage != "ingress" && stage != jsplugin.ReplyHook) || instance.Plugin != "content-moderation" ||
		!slices.Contains(plugin.Meta.RequiredCapabilities, jsplugin.CapabilityModerationAudit) || ctx.Value(isolatedModerationPoolsKey{}) != nil {
		return nil
	}
	requestID, _ := ctx.Value(common.RequestIdKey).(string)
	if requestID == "" {
		return nil
	}
	audit := &ModerationAudit{
		event: model.ContentModerationAudit{RequestID: auditClip(requestID, 128), UserID: userID, InstanceID: auditClip(instance.ID, 128),
			InstanceName: auditClip(instance.Name, 128), Model: auditClip(modelName, 128), Stage: stage, Mode: "enforce", CreatedAt: time.Now().Unix()},
		stage: stage,
	}
	if err := common.Unmarshal(instance.Config, &audit.options); err != nil {
		return nil
	}
	audit.settingsHash = sha256.Sum256(instance.Config)
	audit.eventScope, _ = ctx.Value(moderationEventScopeKey{}).(string)
	audit.observed = audit.options.Mode == "observe"
	if audit.observed {
		audit.event.Mode = "observe"
	}
	for _, user := range audit.options.OnlyRecord {
		if user.UserID == userID {
			audit.observed = true
			audit.event.Mode = "only_record"
			break
		}
	}
	sample := 100
	if audit.options.Sample != nil {
		sample = min(100, max(0, *audit.options.Sample))
	}
	candidate := sha256.Sum256([]byte(requestID + "\x00" + instance.ID))
	audit.sampled = uint64(sample) > binary.BigEndian.Uint64(candidate[:8])%100
	audit.event.Engine, audit.event.EngineModel = "openai", auditClip(audit.options.Model, 128)
	if audit.options.Engine == "typesafe" {
		audit.event.Engine, audit.event.EngineModel = "typesafe", auditClip(audit.options.TypeSafeModel, 128)
	}
	return audit
}

func (audit *ModerationAudit) Sampled() bool { return audit.sampled }

func (audit *ModerationAudit) Observes() bool { return audit.observed }

func (audit *ModerationAudit) HistoryHit() (string, bool) {
	return audit.event.Category, audit.historyHit
}

func (audit *ModerationAudit) Enqueue(size int, run func(context.Context)) bool {
	if size > ModerationObservationMaxBytes {
		audit.Skip("oversize")
		return false
	}
	workers, limit := audit.options.Workers, audit.options.QueueLimit
	if workers <= 0 {
		workers = 2
	}
	if limit <= 0 {
		limit = 64
	}
	accepted := moderationObservations.enqueue(moderationJob{instance: audit.event.InstanceID, workers: min(workers, 8), bytes: size, run: run}, min(limit, 512))
	if !accepted {
		audit.Skip("queue_full")
	}
	return accepted
}

func (audit *ModerationAudit) Skip(reason string) {
	audit.event.Decision, audit.event.FailureClass = model.ModerationAuditSkipped, reason
	audit.store()
}

func (audit *ModerationAudit) ConfigureCapture(plugin *jsplugin.LoadedPlugin, instance *request_plugin_setting.Instance) {
	if audit.categories != nil {
		return
	}
	config := gjson.ParseBytes(instance.Config)
	schema := gjson.ParseBytes(plugin.Meta.ConfigSchemaJSON())
	audit.categories = make(map[string]bool, moderationAuditCategoryLimit)
	audit.thresholds = make(map[string]float64, moderationAuditCategoryLimit)
	audit.scores = make(map[string]float64, moderationAuditCategoryLimit)
	selectedField, thresholdField, endpoint, modelField := "categories", "openAIThresholds", "url", "model"
	if audit.event.Engine == "typesafe" {
		selectedField, thresholdField, endpoint, modelField = "typeSafeCategories", "typeSafeThresholds", "typeSafeUrl", "typeSafeModel"
	}
	for _, category := range schema.Get("properties." + selectedField + ".items.enum").Array() {
		audit.categories[category.Str] = true
	}
	var selected []string
	for _, category := range config.Get(selectedField).Array() {
		selected = append(selected, category.Str)
	}
	if audit.event.Engine == "typesafe" {
		for _, row := range schema.Get("properties.typeSafeThresholds.examples.0").Array() {
			category := row.Get("category").Str
			if len(selected) == 0 || slices.Contains(selected, category) {
				audit.thresholds[category] = row.Get("threshold").Float()
			}
		}
	}
	for _, row := range config.Get(thresholdField).Array() {
		category := row.Get("category").Str
		if audit.categories[category] && (len(selected) == 0 || slices.Contains(selected, category)) {
			audit.thresholds[category] = row.Get("threshold").Float()
		}
	}
	if audit.options.HashPrecheck && audit.stage == "ingress" && audit.event.Mode != "only_record" {
		parts := []any{instance.ID, audit.event.Engine, plugin.Meta.Version, config.Get(endpoint).Str,
			config.Get(modelField).Str, config.Get("scope").Str, config.Get("images").Raw, selected, audit.thresholds}
		material, _ := common.Marshal(parts)
		audit.policyKey = string(material)
	}
}

func (audit *ModerationAudit) Fetcher(fetch jsplugin.Fetcher) jsplugin.Fetcher {
	return func(ctx context.Context, req jsplugin.FetchRequest) (jsplugin.FetchResponse, error) {
		audit.fetchCount++
		payload := gjson.Parse(req.Body)
		input := payload.Get("input")
		if audit.event.Engine == "typesafe" {
			input = payload.Get("state")
		}
		textParts, textOnly := moderationText(input)
		audit.event.InputExcerpt = ""
		if audit.options.KeepExcerpt && len(textParts) > 0 {
			audit.event.InputExcerpt = redactAuditExcerpt(strings.Join(textParts, "\n"))
		}
		audit.event.RiskHash = ""
		if audit.policyKey != "" && textOnly && len(textParts) > 0 && req.BodyStream == nil {
			hash := hmac.New(sha256.New, []byte(common.CryptoSecret))
			_, _ = hash.Write([]byte(audit.policyKey))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write([]byte(input.Raw))
			audit.event.RiskHash = hex.EncodeToString(hash.Sum(nil))
			if !audit.observed && model.DB != nil {
				lookup, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
				category, hit, err := model.ContentModerationHistory(lookup, audit.event.RiskHash, audit.event.CreatedAt)
				cancel()
				if err == nil && hit {
					audit.historyHit, audit.event.Category = true, category
					return jsplugin.FetchResponse{}, errors.New("known moderation match")
				}
			}
		}
		response, err := fetch(ctx, req)
		if err == nil && response.Status >= 200 && response.Status < 300 && len(response.Body) <= jsplugin.MaxFetchBodyBytes {
			audit.captureScores(response.Body)
		}
		return response, err
	}
}

func moderationText(input gjson.Result) ([]string, bool) {
	if input.Type == gjson.String {
		return []string{input.Str}, true
	}
	if !input.IsArray() {
		return nil, false
	}
	var parts []string
	textOnly := true
	for _, part := range input.Array() {
		if part.Type == gjson.String {
			parts = append(parts, part.Str)
		} else if part.Get("type").Str == "text" && part.Get("text").Type == gjson.String {
			parts = append(parts, part.Get("text").Str)
		} else {
			textOnly = false
		}
	}
	return parts, textOnly
}

func (audit *ModerationAudit) captureScores(raw []byte) {
	response := gjson.ParseBytes(raw)
	if audit.event.Engine == "typesafe" {
		for name, answer := range response.Get("answers").Map() {
			if audit.categories[name] {
				audit.addScore(name, answer.Get("noul"))
			}
		}
		return
	}
	if audit.event.Engine == "openai" {
		for _, result := range response.Get("results").Array() {
			for name, score := range result.Get("category_scores").Map() {
				if audit.categories[name] {
					audit.addScore(name, score)
				}
			}
		}
	}
}

func (audit *ModerationAudit) addScore(name string, score gjson.Result) {
	if score.Type == gjson.Number && score.Num >= 0 && score.Num <= 1 {
		audit.scores[name] = max(score.Num, audit.scores[name])
	}
}

func (audit *ModerationAudit) Record(action, code, category string, errored bool, skipped bool) {
	switch {
	case audit.historyHit:
		audit.event.Decision = model.ModerationAuditHistory
	case errored || skipped:
		audit.event.Decision, audit.event.FailureClass = model.ModerationAuditFailure, "audit_failed"
	case action == "reject" || action == "stop":
		if !audit.categories[category] && category != "flagged" {
			category = ""
		}
		audit.event.Category = category
		audit.event.Decision = model.ModerationAuditBlock
		if audit.observed {
			audit.event.Decision = model.ModerationAuditHit
		}
		if code == "moderation.too_long" {
			audit.event.FailureClass = "input_too_long"
		}
	default:
		if audit.fetchCount == 0 || !audit.options.RecordPass {
			return
		}
		audit.event.Decision = model.ModerationAuditPass
	}
	if len(audit.scores) > 0 {
		encoded, _ := common.Marshal(audit.scores)
		audit.event.Scores = string(encoded)
	}
	if len(audit.thresholds) > 0 {
		encoded, _ := common.Marshal(audit.thresholds)
		audit.event.Thresholds = string(encoded)
	}
	audit.store()
}

func (audit *ModerationAudit) store() {
	if model.DB == nil {
		return
	}
	event := audit.event
	parts := []string{event.RequestID, strconv.Itoa(event.UserID), event.InstanceID, event.Stage, event.Decision, audit.eventScope}
	material, _ := common.Marshal(parts)
	key := sha256.Sum256(material)
	event.EventKey = hex.EncodeToString(key[:])
	days := audit.options.NonHitDays
	if days == 0 {
		days = 3
	}
	days = max(1, min(days, 3))
	if event.Decision == model.ModerationAuditBlock || event.Decision == model.ModerationAuditHit || event.Decision == model.ModerationAuditHistory {
		days = audit.options.HitDays
		if days == 0 {
			days = 180
		}
		days = max(1, min(days, 3650))
	}
	event.ExpiresAt = event.CreatedAt + int64(days)*86400
	options, settingsHash := audit.options, audit.settingsHash
	moderationRecords.enqueue(moderationJob{instance: event.InstanceID, workers: 2,
		bytes: len(event.InputExcerpt) + len(event.Scores) + len(event.Thresholds) + 1024, run: func(ctx context.Context) {
			if err := model.StoreContentModerationAudit(ctx, &event); err != nil {
				logger.LogWarn(ctx, common.LogText("failed to store a content moderation audit"))
				return
			}
			applyContentModerationActions(ctx, &event, options, settingsHash)
		}}, 512)
}

var moderationCredentialPattern = regexp.MustCompile(`(?i)\b(?:authorization\s*[:=]\s*(?:bearer\s+)?|bearer\s+|(?:api[-_ ]?key|password|token|secret)\s*["']?\s*[:=]\s*["']?)[^\s,;"']+|\b(?:sk-|gh[pousr]_|github_pat_)[a-z0-9_-]{8,}|-----BEGIN [^-\r\n]*PRIVATE KEY-----[\s\S]*`)

func redactAuditExcerpt(text string) string {
	text = moderationCredentialPattern.ReplaceAllString(common.MaskSensitiveInfo(text), "[redacted]")
	return auditClip(strings.Join(strings.FieldsFunc(text, func(value rune) bool { return unicode.IsSpace(value) || unicode.IsControl(value) }), " "), 240)
}

func auditClip(text string, limit int) string {
	count := 0
	for offset := range text {
		if count == limit {
			return strings.Clone(text[:offset])
		}
		count++
	}
	return strings.Clone(text)
}

func StartContentModerationRetention() {
	if !common.IsMasterNode && os.Getenv("CONTENT_MODERATION_RETENTION_OWNER") != "true" {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for now := range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := model.DeleteExpiredContentModerationAudits(ctx, now.Unix(), 500)
			if err == nil {
				err = model.DeleteExpiredModerationIncidents(ctx, now.Unix())
			}
			cancel()
			if err != nil {
				logger.LogWarn(context.Background(), common.LogText("content moderation retention cleanup failed"))
			}
		}
	}()
}
