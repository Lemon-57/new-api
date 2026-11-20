package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"golang.org/x/net/http/httpguts"
)

const maxModerationKeyPools = 1024

type moderationKeySet struct {
	endpoint string
	engine   string
	keys     []string
	timeout  time.Duration
	budget   time.Duration
	attempts int
	id       [32]byte
}

type moderationKeyState struct {
	Slot       int       `json:"slot"`
	StatusCode int       `json:"status_code,omitempty"`
	Result     string    `json:"result"`
	CheckedAt  time.Time `json:"checked_at,omitzero"`
	RetryAt    time.Time `json:"retry_at,omitzero"`
	started    time.Time
}

type moderationKeyPool struct {
	next int
	used time.Time
	keys []moderationKeyState
}

type moderationKeyPools struct {
	mu    sync.Mutex
	pools map[[32]byte]*moderationKeyPool
}

var liveModerationKeyPools = &moderationKeyPools{pools: make(map[[32]byte]*moderationKeyPool)}

type isolatedModerationPoolsKey struct{}

func WithIsolatedModerationKeyPool(ctx context.Context) context.Context {
	return context.WithValue(ctx, isolatedModerationPoolsKey{}, &moderationKeyPools{pools: make(map[[32]byte]*moderationKeyPool)})
}

func moderationKeys(raw json.RawMessage) *moderationKeySet {
	var config struct {
		Engine          string                 `json:"engine"`
		URL             string                 `json:"url"`
		APIKey          string                 `json:"apiKey"`
		APIKeys         []struct{ Key string } `json:"apiKeys"`
		TypeSafeURL     string                 `json:"typeSafeUrl"`
		TypeSafeAPIKey  string                 `json:"typeSafeApiKey"`
		TypeSafeAPIKeys []struct{ Key string } `json:"typeSafeApiKeys"`
		TimeoutMs       int                    `json:"auditTimeoutMs"`
		BudgetMs        int                    `json:"auditTotalTimeoutMs"`
		RetryCount      *int                   `json:"auditRetryCount"`
	}
	if err := common.Unmarshal(raw, &config); err != nil {
		return nil
	}
	endpoint, engine, primary, additional := config.URL, "openai", config.APIKey, config.APIKeys
	if config.Engine == "typesafe" {
		endpoint, engine, primary, additional = config.TypeSafeURL, "typesafe", config.TypeSafeAPIKey, config.TypeSafeAPIKeys
	}
	if endpoint == "" {
		return nil
	}
	keys := make([]string, 0, len(additional)+1)
	for _, item := range append([]struct{ Key string }{{Key: primary}}, additional...) {
		key := strings.TrimSpace(item.Key)
		if key != "" && httpguts.ValidHeaderFieldValue(key) && !strings.ContainsAny(key, " \t") && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
		if len(keys) >= 100 {
			break
		}
	}
	if len(keys) == 0 {
		keys = []string{""}
	}
	identity, err := common.Marshal([]any{engine, endpoint, keys})
	if err != nil {
		return nil
	}
	set := &moderationKeySet{endpoint: endpoint, engine: engine, keys: keys, id: sha256.Sum256(identity), attempts: 1}
	if len(additional) > 0 || config.TimeoutMs > 0 || config.BudgetMs > 0 || config.RetryCount != nil {
		set.timeout = 3 * time.Second
		if config.TimeoutMs > 0 {
			set.timeout = time.Duration(min(config.TimeoutMs, 15000)) * time.Millisecond
		}
		set.attempts = 3
		if config.RetryCount != nil {
			set.attempts = min(max(*config.RetryCount, 0), 5) + 1
		}
		set.budget = min(time.Duration(set.attempts)*set.timeout, 15*time.Second)
		if config.BudgetMs > 0 {
			set.budget = time.Duration(min(config.BudgetMs, 15000)) * time.Millisecond
		}
	}
	return set
}

func (pools *moderationKeyPools) choose(set *moderationKeySet, now time.Time) (int, bool) {
	pools.mu.Lock()
	defer pools.mu.Unlock()
	pool := pools.pools[set.id]
	if pool == nil {
		if len(pools.pools) >= maxModerationKeyPools {
			var oldest [32]byte
			var used time.Time
			for id, existing := range pools.pools {
				if used.IsZero() || existing.used.Before(used) {
					oldest, used = id, existing.used
				}
			}
			delete(pools.pools, oldest)
		}
		pool = &moderationKeyPool{keys: make([]moderationKeyState, len(set.keys))}
		pools.pools[set.id] = pool
	}
	pool.used = now
	for range len(pool.keys) {
		index := pool.next
		pool.next = (index + 1) % len(pool.keys)
		if set.budget == 0 || !pool.keys[index].RetryAt.After(now) {
			return index, true
		}
	}
	return 0, false
}

func (pools *moderationKeyPools) record(set *moderationKeySet, index int, started time.Time, status int, result string) {
	pools.mu.Lock()
	defer pools.mu.Unlock()
	pool := pools.pools[set.id]
	if pool == nil || started.Before(pool.keys[index].started) {
		return
	}
	state := moderationKeyState{StatusCode: status, Result: result, CheckedAt: time.Now(), started: started}
	if set.budget > 0 && set.keys[index] != "" {
		switch status {
		case http.StatusUnauthorized, http.StatusForbidden:
			state.RetryAt = state.CheckedAt.Add(10 * time.Minute)
		case http.StatusTooManyRequests:
			state.RetryAt = state.CheckedAt.Add(time.Minute)
		}
	}
	pool.keys[index] = state
}

func (set *moderationKeySet) wrap(send jsplugin.Fetcher) jsplugin.Fetcher {
	var deadline time.Time
	return func(ctx context.Context, request jsplugin.FetchRequest) (jsplugin.FetchResponse, error) {
		if request.Method != http.MethodPost || request.URL != set.endpoint || (request.Hook != "ingress" && request.Hook != jsplugin.ReplyHook) {
			return jsplugin.FetchResponse{}, errors.New("moderation must use the configured audit endpoint")
		}
		pools, ok := ctx.Value(isolatedModerationPoolsKey{}).(*moderationKeyPools)
		if !ok {
			pools = liveModerationKeyPools
		}
		if set.budget == 0 {
			started := time.Now()
			index, _ := pools.choose(set, started)
			response, err := send(ctx, request)
			pools.record(set, index, started, response.Status, moderationFetchResult(set.engine, request.Body, response, err))
			return response, err
		}
		if deadline.IsZero() {
			deadline = time.Now().Add(set.budget)
		}
		budget, cancelBudget := context.WithDeadline(ctx, deadline)
		defer cancelBudget()
		request.Headers = maps.Clone(request.Headers)
		if request.Headers == nil {
			request.Headers = make(map[string]string)
		}
		for header := range request.Headers {
			if strings.EqualFold(header, "Authorization") {
				delete(request.Headers, header)
			}
		}
		var failure error
		for attempt := range set.attempts {
			if budget.Err() != nil {
				return jsplugin.FetchResponse{}, errors.New("moderation API exhausted its time budget")
			}
			if attempt > 0 && request.BodyStream != nil {
				if request.ReplayBody == nil {
					break
				}
				body, size, err := request.ReplayBody()
				if err != nil {
					return jsplugin.FetchResponse{}, errors.New("moderation input could not be replayed")
				}
				request.BodyStream, request.BodySize = body, size
			}
			started := time.Now()
			index, usable := pools.choose(set, started)
			if !usable {
				if failure == nil {
					failure = errors.New("no moderation API key available")
				}
				break
			}
			delete(request.Headers, "Authorization")
			if key := set.keys[index]; key != "" {
				request.Headers["Authorization"] = "Bearer " + key
			}
			attemptCtx, cancel := context.WithTimeout(budget, set.timeout)
			response, err := send(attemptCtx, request)
			if attemptCtx.Err() != nil {
				err = attemptCtx.Err()
			}
			cancel()
			result := moderationFetchResult(set.engine, request.Body, response, err)
			pools.record(set, index, started, response.Status, result)
			if result == "ok" {
				return response, nil
			}
			failure = fmt.Errorf("moderation API %s", result)
			if result == "http_error" {
				failure = fmt.Errorf("moderation API returned HTTP %d", response.Status)
				status := response.Status
				if status != http.StatusUnauthorized && status != http.StatusForbidden && status != http.StatusTooManyRequests && status != http.StatusRequestTimeout && status < 500 {
					break
				}
			}
		}
		return jsplugin.FetchResponse{}, failure
	}
}

func moderationFetchResult(engine, requestBody string, response jsplugin.FetchResponse, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if err != nil {
		return "transport_error"
	}
	if response.Status < 200 || response.Status >= 300 {
		return "http_error"
	}
	if len(response.Body) > jsplugin.MaxFetchBodyBytes {
		return "invalid_response"
	}
	if engine == "typesafe" {
		var input struct{ Questions map[string]json.RawMessage }
		var output struct {
			Answers map[string]struct {
				Type string
				Noul *float64
			}
		}
		if common.UnmarshalJsonStr(requestBody, &input) != nil || len(input.Questions) == 0 || common.Unmarshal(response.Body, &output) != nil {
			return "invalid_response"
		}
		for category := range input.Questions {
			answer := output.Answers[category]
			if answer.Type != "noul" || answer.Noul == nil || *answer.Noul < 0 || *answer.Noul > 1 {
				return "invalid_response"
			}
		}
		return "ok"
	}
	var output struct{ Results []map[string]json.RawMessage }
	if common.Unmarshal(response.Body, &output) != nil || len(output.Results) == 0 || slices.ContainsFunc(output.Results, func(result map[string]json.RawMessage) bool { return result == nil }) {
		return "invalid_response"
	}
	return "ok"
}

func ModerationKeyHealth(config json.RawMessage) (string, []moderationKeyState) {
	set := moderationKeys(config)
	if set == nil {
		return "", nil
	}
	liveModerationKeyPools.mu.Lock()
	defer liveModerationKeyPools.mu.Unlock()
	states := make([]moderationKeyState, len(set.keys))
	if pool := liveModerationKeyPools.pools[set.id]; pool != nil {
		copy(states, pool.keys)
	}
	for index := range states {
		states[index].Slot = index + 1
		if states[index].Result == "" {
			states[index].Result = "not_checked"
		}
	}
	return set.engine, states
}
