package setting

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const (
	ContentModerationEnabledKey = "ContentModerationEnabled"
	ContentModerationEngineKey  = "ContentModerationEngine"
	ContentModerationModeKey    = "ContentModerationMode"
	ContentModerationBaseURLKey = "ContentModerationBaseURL"
	ContentModerationModelKey   = "ContentModerationModel"
	ContentModerationAPIKeyKey  = "ContentModerationAPIKey"
	// Write-only commands; never persist these alongside the credential.
	ContentModerationKeyUpdateModeKey = "ContentModerationKeyUpdateMode"
	ContentModerationDeleteKeyKey     = "ContentModerationDeleteKey"
	ContentModerationKeyConfiguredKey = "ContentModerationKeyConfigured"
	ContentModerationTimeoutMSKey     = "ContentModerationTimeoutMS"
	ContentModerationRetryCountKey    = "ContentModerationRetryCount"
	ContentModerationSampleRateKey    = "ContentModerationSampleRate"
	ContentModerationFailOpenKey      = "ContentModerationFailOpen"
	ContentModerationThresholdsKey    = "ContentModerationThresholds"
)

const (
	ContentModerationEngineTypeSafe = "typesafe"
	ContentModerationModePreBlock   = "pre_block"
	ContentModerationDefaultBaseURL = "https://jev.bocha.cn"
	ContentModerationDefaultModel   = "bocha-jev-v1"
)

var (
	ContentModerationEnabled    = false
	ContentModerationEngine     = ContentModerationEngineTypeSafe
	ContentModerationMode       = ContentModerationModePreBlock
	ContentModerationBaseURL    = ContentModerationDefaultBaseURL
	ContentModerationModel      = ContentModerationDefaultModel
	ContentModerationAPIKey     = ""
	ContentModerationTimeoutMS  = 3000
	ContentModerationRetryCount = 2
	ContentModerationSampleRate = 100
	ContentModerationFailOpen   = false
	ContentModerationThresholds = map[string]float64{
		"harassment":             0.98,
		"harassment/threatening": 0.9,
		"hate":                   0.65,
		"hate/threatening":       0.65,
		"illicit":                0.95,
		"illicit/violent":        0.95,
		"self-harm":              0.65,
		"self-harm/instructions": 0.65,
		"self-harm/intent":       0.85,
		"sexual":                 0.65,
		"sexual/minors":          0.65,
		"violence":               0.95,
		"violence/graphic":       0.95,
	}
)

var contentModerationMu sync.RWMutex
var contentModerationUpdateMu sync.RWMutex

// LockContentModerationUpdates keeps requests from observing a partially
// applied management transaction or database reload. Callers release it after
// all runtime options have been applied, including the decrypted credential.
func LockContentModerationUpdates() func() {
	contentModerationUpdateMu.Lock()
	return contentModerationUpdateMu.Unlock
}

type ContentModerationConfig struct {
	Enabled    bool
	Engine     string
	Mode       string
	BaseURL    string
	Model      string
	APIKey     string
	TimeoutMS  int
	RetryCount int
	SampleRate int
	FailOpen   bool
	Thresholds map[string]float64
}

func GetContentModerationConfig() ContentModerationConfig {
	contentModerationUpdateMu.RLock()
	defer contentModerationUpdateMu.RUnlock()
	contentModerationMu.RLock()
	defer contentModerationMu.RUnlock()
	thresholds := make(map[string]float64, len(ContentModerationThresholds))
	for key, value := range ContentModerationThresholds {
		thresholds[key] = value
	}
	return ContentModerationConfig{
		Enabled:    ContentModerationEnabled,
		Engine:     ContentModerationEngine,
		Mode:       ContentModerationMode,
		BaseURL:    ContentModerationBaseURL,
		Model:      ContentModerationModel,
		APIKey:     ContentModerationAPIKey,
		TimeoutMS:  ContentModerationTimeoutMS,
		RetryCount: ContentModerationRetryCount,
		SampleRate: ContentModerationSampleRate,
		FailOpen:   ContentModerationFailOpen,
		Thresholds: thresholds,
	}
}

// APIKeys accepts the legacy single credential and the newline-delimited pool.
// Keep the existing encrypted option so upgrades require no schema migration.
func (c ContentModerationConfig) APIKeys() []string {
	keys, _ := NormalizeContentModerationKeys(c.APIKey)
	return keys
}

func NormalizeContentModerationKeys(value string) ([]string, error) {
	if len(value) > 65536 {
		return nil, errors.New("moderation key pool exceeds size limit")
	}
	keys := make([]string, 0)
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(value, "\n") {
		key := strings.TrimSpace(line)
		if key == "" || seen[key] {
			continue
		}
		if len(key) > 8192 || strings.ContainsAny(key, "\r\t ,") {
			return nil, errors.New("invalid moderation key; enter one key per line")
		}
		for _, r := range key {
			if r < 33 || r > 126 {
				return nil, errors.New("invalid moderation key")
			}
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) > 100 {
		return nil, errors.New("moderation key pool allows at most 100 keys")
	}
	return keys, nil
}

func SetContentModerationOption(key, value string) error {
	if err := ValidateContentModerationOption(key, value); err != nil {
		return err
	}
	contentModerationMu.Lock()
	defer contentModerationMu.Unlock()
	switch key {
	case ContentModerationEnabledKey:
		ContentModerationEnabled = value == "true"
	case ContentModerationEngineKey:
		if value != ContentModerationEngineTypeSafe {
			return errors.New("unsupported content moderation engine")
		}
		ContentModerationEngine = value
	case ContentModerationModeKey:
		if value != ContentModerationModePreBlock && value != "observe" {
			return errors.New("unsupported content moderation mode")
		}
		ContentModerationMode = value
	case ContentModerationBaseURLKey:
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			value = ContentModerationDefaultBaseURL
		}
		ContentModerationBaseURL = value
	case ContentModerationModelKey:
		value = strings.TrimSpace(value)
		if value == "" {
			value = ContentModerationDefaultModel
		}
		ContentModerationModel = value
	case ContentModerationAPIKeyKey:
		keys, _ := NormalizeContentModerationKeys(value)
		ContentModerationAPIKey = strings.Join(keys, "\n")
	case ContentModerationTimeoutMSKey:
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 100 || parsed > 60000 {
			return errors.New("content moderation timeout must be between 100 and 60000 ms")
		}
		ContentModerationTimeoutMS = parsed
	case ContentModerationRetryCountKey:
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 5 {
			return errors.New("content moderation retry count must be between 0 and 5")
		}
		ContentModerationRetryCount = parsed
	case ContentModerationSampleRateKey:
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 100 {
			return errors.New("content moderation sample rate must be between 0 and 100")
		}
		ContentModerationSampleRate = parsed
	case ContentModerationFailOpenKey:
		ContentModerationFailOpen = value == "true"
	case ContentModerationThresholdsKey:
		var parsed map[string]float64
		if err := common.UnmarshalJsonStr(value, &parsed); err != nil {
			return errors.New("invalid content moderation thresholds")
		}
		for category, threshold := range parsed {
			if threshold < 0 || threshold > 1 || strings.TrimSpace(category) == "" {
				return errors.New("content moderation thresholds must be between 0 and 1")
			}
		}
		ContentModerationThresholds = parsed
	}
	return nil
}

// ValidateContentModerationOption is side-effect free so management writes can
// reject the complete update before opening a database transaction.
func ValidateContentModerationOption(key, value string) error {
	switch key {
	case ContentModerationEnabledKey, ContentModerationFailOpenKey:
		if value != "true" && value != "false" {
			return errors.New("invalid moderation boolean")
		}
	case ContentModerationEngineKey:
		if value != ContentModerationEngineTypeSafe {
			return errors.New("unsupported moderation engine")
		}
	case ContentModerationModeKey:
		if value != "observe" && value != ContentModerationModePreBlock {
			return errors.New("unsupported moderation mode")
		}
	case ContentModerationBaseURLKey:
		u, err := url.Parse(strings.TrimSpace(value))
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
			return errors.New("moderation URL must use HTTPS (HTTP is allowed only for local tests)")
		}
	case ContentModerationModelKey:
		if strings.TrimSpace(value) == "" || len(value) > 200 {
			return errors.New("invalid moderation model")
		}
	case ContentModerationAPIKeyKey:
		_, err := NormalizeContentModerationKeys(value)
		return err
	case ContentModerationTimeoutMSKey, ContentModerationRetryCountKey, ContentModerationSampleRateKey:
		low, high := 0, 100
		if key == ContentModerationTimeoutMSKey {
			low, high = 100, 60000
		}
		if key == ContentModerationRetryCountKey {
			high = 5
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < low || n > high {
			return errors.New("moderation numeric setting is out of range")
		}
	case ContentModerationThresholdsKey:
		var values map[string]*float64
		if err := common.UnmarshalJsonStr(value, &values); err != nil || len(values) == 0 {
			return errors.New("moderation thresholds must be a nonempty object")
		}
		for category, score := range values {
			switch category {
			case "harassment", "harassment/threatening", "hate", "hate/threatening", "illicit", "illicit/violent", "self-harm", "self-harm/intent", "self-harm/instructions", "sexual", "sexual/minors", "violence", "violence/graphic":
			default:
				return errors.New("unsupported moderation category")
			}
			if score == nil || *score < 0 || *score > 1 {
				return errors.New("invalid moderation threshold")
			}
		}
	}
	return nil
}
