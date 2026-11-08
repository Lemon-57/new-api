package setting

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// Health is process-local, as in Sub2API. Never retain credentials in this map.
type ContentModerationKeyStatus struct {
	Hash           string    `json:"hash"`
	Masked         string    `json:"masked"`
	FrozenUntil    time.Time `json:"frozen_until"`
	LastCheckedAt  time.Time `json:"last_checked_at"`
	LastHTTPStatus int       `json:"last_http_status"`
	LastLatencyMS  int64     `json:"last_latency_ms"`
	LastError      string    `json:"last_error"`
	Calls          uint64    `json:"calls"`
	Successes      uint64    `json:"successes"`
	Errors         uint64    `json:"errors"`
	Active         int       `json:"active"`
	LatencyMS      int64     `json:"latency_ms"`
}

var moderationKeyPool = struct {
	sync.Mutex
	cursor uint64
	states map[string]*ContentModerationKeyStatus
}{states: make(map[string]*ContentModerationKeyStatus)}

func ContentModerationKeyHash(key string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("typesafe\x00"+key)))
}

func moderationKeyScope(c ContentModerationConfig, key string) string {
	return c.BaseURL + "\x00" + c.Model + "\x00" + ContentModerationKeyHash(key)
}

// Caller holds moderationKeyPool's mutex.
func moderationKeyState(c ContentModerationConfig, key string) *ContentModerationKeyStatus {
	id := moderationKeyScope(c, key)
	state := moderationKeyPool.states[id]
	if state == nil {
		masked := "****"
		if len(key) > 8 {
			masked += key[len(key)-4:]
		}
		state = &ContentModerationKeyStatus{Hash: ContentModerationKeyHash(key), Masked: masked}
		moderationKeyPool.states[id] = state
	}
	return state
}

// BeginContentModerationKey selects and accounts for an attempt atomically.
func BeginContentModerationKey(c ContentModerationConfig) (string, bool) {
	keys := c.APIKeys()
	moderationKeyPool.Lock()
	defer moderationKeyPool.Unlock()
	for range len(keys) {
		key := keys[moderationKeyPool.cursor%uint64(len(keys))]
		moderationKeyPool.cursor++
		state := moderationKeyState(c, key)
		if state.FrozenUntil.After(time.Now()) {
			continue
		}
		state.Active++
		return key, true
	}
	return "", false
}

func FinishContentModerationKey(c ContentModerationConfig, key string, status int, elapsed time.Duration, callErr error) {
	moderationKeyPool.Lock()
	defer moderationKeyPool.Unlock()
	state := moderationKeyState(c, key)
	state.Active = max(0, state.Active-1)
	state.Calls++
	state.LastCheckedAt = time.Now()
	state.LastHTTPStatus = status
	state.LastLatencyMS = max(0, elapsed.Milliseconds())
	state.LatencyMS += state.LastLatencyMS
	if callErr == nil {
		state.Successes++
		state.LastError = ""
		state.FrozenUntil = time.Time{}
		return
	}
	state.Errors++
	// Never retain upstream bodies or transport errors (which can contain secrets).
	state.LastError = "Moderation request failed"
	duration := time.Duration(0)
	switch status {
	case 0, 400:
	case 401, 403:
		duration = 10 * time.Minute
	case 429, 529:
		duration = time.Minute
	default:
		duration = 10 * time.Second
	}
	if duration > 0 {
		state.FrozenUntil = time.Now().Add(duration)
	}
}

func ContentModerationKeyStatuses(c ContentModerationConfig) []ContentModerationKeyStatus {
	keys := c.APIKeys()
	moderationKeyPool.Lock()
	defer moderationKeyPool.Unlock()
	result := make([]ContentModerationKeyStatus, 0, len(keys))
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		result = append(result, *moderationKeyState(c, key))
		allowed[moderationKeyScope(c, key)] = true
	}
	for id, state := range moderationKeyPool.states {
		if !allowed[id] && state.Active == 0 {
			delete(moderationKeyPool.states, id)
		}
	}
	return result
}
