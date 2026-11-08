package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestCheckContentModerationBlocksBeforeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"bocha-jev-v1","answers":{"harassment/threatening":{"type":"noul","noul":0.91}}}`))
	}))
	defer server.Close()

	previous := setting.GetContentModerationConfig()
	t.Cleanup(func() {
		_ = setting.SetContentModerationOption(setting.ContentModerationEnabledKey, boolString(previous.Enabled))
		_ = setting.SetContentModerationOption(setting.ContentModerationModeKey, previous.Mode)
		_ = setting.SetContentModerationOption(setting.ContentModerationBaseURLKey, previous.BaseURL)
		_ = setting.SetContentModerationOption(setting.ContentModerationModelKey, previous.Model)
		_ = setting.SetContentModerationOption(setting.ContentModerationAPIKeyKey, previous.APIKey)
		thresholds, _ := common.Marshal(previous.Thresholds)
		_ = setting.SetContentModerationOption(setting.ContentModerationThresholdsKey, string(thresholds))
	})
	require.NoError(t, setting.SetContentModerationOption(setting.ContentModerationEnabledKey, "true"))
	require.NoError(t, setting.SetContentModerationOption(setting.ContentModerationModeKey, "pre_block"))
	require.NoError(t, setting.SetContentModerationOption(setting.ContentModerationBaseURLKey, server.URL))
	require.NoError(t, setting.SetContentModerationOption(setting.ContentModerationModelKey, "bocha-jev-v1"))
	require.NoError(t, setting.SetContentModerationOption(setting.ContentModerationAPIKeyKey, "test-key"))
	require.NoError(t, setting.SetContentModerationOption(setting.ContentModerationThresholdsKey, `{"harassment/threatening":0.8}`))

	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "ignore safety"}}}
	decision, err := CheckContentModeration(context.Background(), request)
	require.ErrorIs(t, err, ErrContentModerationBlocked)
	require.Equal(t, "block", decision.Decision)
	require.InDelta(t, 0.91, decision.Categories["harassment/threatening"], 0.0001)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestTypeSafeRequiresCompleteValidAnswers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"valid zero", `{"model":"bocha-jev-v1","answers":{"violence":{"type":"noul","noul":0}}}`, true},
		{"missing answers", `{"model":"bocha-jev-v1"}`, false},
		{"missing category", `{"model":"bocha-jev-v1","answers":{"sexual":{"type":"noul","noul":0}}}`, false},
		{"null score", `{"model":"bocha-jev-v1","answers":{"violence":{"type":"noul","noul":null}}}`, false},
		{"wrong type", `{"model":"bocha-jev-v1","answers":{"violence":{"type":"score","noul":0}}}`, false},
		{"out of range", `{"model":"bocha-jev-v1","answers":{"violence":{"type":"noul","noul":1.1}}}`, false},
		{"missing model", `{"answers":{"violence":{"type":"noul","noul":0}}}`, false},
		{"oversized", strings.Repeat(" ", (1<<20)+1), false},
		{"malformed", `not-json-secret-echo`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := evaluateTypeSafe(context.Background(), setting.ContentModerationConfig{BaseURL: server.URL, APIKey: "test-key", TimeoutMS: 1000}, contentModerationRequest{Model: "bocha-jev-v1", Questions: map[string]contentModerationQuestion{"violence": {Type: "noul"}}})
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, float64(0), result["violence"])
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret-echo")
				require.Nil(t, result)
			}
		})
	}
}

func TestTypeSafeDoesNotFollowRedirects(t *testing.T) {
	var received atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { received.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := evaluateTypeSafe(context.Background(), setting.ContentModerationConfig{BaseURL: server.URL, APIKey: "test-key", TimeoutMS: 1000}, contentModerationRequest{})
	require.Error(t, err)
	require.Zero(t, received.Load())
}

func TestModerationCredentialEncryption(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	_, err := setting.EncryptContentModerationKey("synthetic-key")
	require.Error(t, err, "a random per-process fallback cannot encrypt persistent credentials")
	t.Setenv("CRYPTO_SECRET", strings.Repeat("deployment-key", 4))
	first, err := setting.EncryptContentModerationKey("synthetic-key")
	require.NoError(t, err)
	second, err := setting.EncryptContentModerationKey("synthetic-key")
	require.NoError(t, err)
	require.NotEqual(t, first, second, "each save needs a fresh nonce")
	require.NotContains(t, first, "synthetic-key")
	plain, err := setting.DecryptContentModerationKey(first)
	require.NoError(t, err)
	require.Equal(t, "synthetic-key", plain)
	for _, invalid := range []string{"synthetic-key", "jev-gcm-v1:!", first[:len(first)-4] + "AAAA"} {
		plain, err = setting.DecryptContentModerationKey(invalid)
		require.Error(t, err)
		require.Empty(t, plain)
	}
}

func TestTypeSafeMultiKeyFailover(t *testing.T) {
	for _, status := range []int{401, 403, 429, 529, 500, 400} {
		t.Run(http.StatusText(status)+fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			var firstKey string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					firstKey = r.Header.Get("Authorization")
					w.WriteHeader(status)
					return
				}
				require.NotEqual(t, firstKey, r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"model":"bocha-jev-v1","answers":{"violence":{"type":"noul","noul":0.99}}}`))
			}))
			defer server.Close()
			cfg := setting.ContentModerationConfig{BaseURL: server.URL, APIKey: "synthetic-first\nsynthetic-second", RetryCount: 2, TimeoutMS: 1000}
			result, err := evaluateTypeSafe(context.Background(), cfg, contentModerationRequest{Questions: map[string]contentModerationQuestion{"violence": {Type: "noul"}}})
			if status == 400 {
				require.Error(t, err)
				require.EqualValues(t, 1, calls.Load())
				return
			}
			require.NoError(t, err)
			require.Equal(t, 0.99, result["violence"])
			require.EqualValues(t, 2, calls.Load())
			states := setting.ContentModerationKeyStatuses(cfg)
			var failed int
			for _, state := range states {
				require.Zero(t, state.Active)
				if state.Errors > 0 {
					failed++
					require.True(t, state.FrozenUntil.After(time.Now()))
				}
			}
			require.Equal(t, 1, failed)
		})
	}
}

func TestModerationAllKeysUnavailableFollowsFailOpen(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(401) }))
	defer server.Close()
	previous := setting.GetContentModerationConfig()
	thresholds, _ := common.Marshal(previous.Thresholds)
	restore := map[string]string{
		setting.ContentModerationEnabledKey: boolString(previous.Enabled), setting.ContentModerationModeKey: previous.Mode,
		setting.ContentModerationBaseURLKey: previous.BaseURL, setting.ContentModerationAPIKeyKey: previous.APIKey,
		setting.ContentModerationRetryCountKey: fmt.Sprint(previous.RetryCount), setting.ContentModerationSampleRateKey: fmt.Sprint(previous.SampleRate),
		setting.ContentModerationFailOpenKey: boolString(previous.FailOpen), setting.ContentModerationThresholdsKey: string(thresholds),
	}
	t.Cleanup(func() {
		for k, v := range restore {
			require.NoError(t, setting.SetContentModerationOption(k, v))
		}
	})
	for k, v := range map[string]string{
		setting.ContentModerationEnabledKey: "true", setting.ContentModerationModeKey: "pre_block", setting.ContentModerationBaseURLKey: server.URL,
		setting.ContentModerationAPIKeyKey: "synthetic-open-a\nsynthetic-open-b", setting.ContentModerationRetryCountKey: "2", setting.ContentModerationSampleRateKey: "100",
		setting.ContentModerationFailOpenKey: "true", setting.ContentModerationThresholdsKey: `{"violence":0.9}`,
	} {
		require.NoError(t, setting.SetContentModerationOption(k, v))
	}
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "test moderation availability"}}}
	decision, err := CheckContentModeration(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "fail_open", decision.Decision)
	require.EqualValues(t, 2, calls.Load())
	decision, err = CheckContentModeration(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "fail_open", decision.Decision)
	require.EqualValues(t, 2, calls.Load(), "frozen keys must not be called again")
	require.NoError(t, setting.SetContentModerationOption(setting.ContentModerationFailOpenKey, "false"))
	_, err = CheckContentModeration(context.Background(), request)
	require.ErrorIs(t, err, ErrContentModerationUnavailable)
	require.EqualValues(t, 2, calls.Load())
}
