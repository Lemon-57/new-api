package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationPoolRotatesAndReplaysMedia(t *testing.T) {
	const endpoint = "https://openai.example.invalid/v1/moderations"
	const media = `{"input":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}`
	set := moderationKeys(json.RawMessage(`{"url":"` + endpoint + `","apiKey":"fixture-primary","apiKeys":[{"key":"fixture-backup"}],"auditRetryCount":1}`))
	require.NotNil(t, set)
	ctx := WithIsolatedModerationKeyPool(t.Context())
	var attempts []string
	fetch := set.wrap(func(_ context.Context, request jsplugin.FetchRequest) (jsplugin.FetchResponse, error) {
		raw, err := io.ReadAll(request.BodyStream)
		require.NoError(t, err)
		assert.Equal(t, media, string(raw))
		assert.Equal(t, int64(len(media)), request.BodySize)
		assert.NotContains(t, request.Headers, "authorization")
		attempts = append(attempts, request.Headers["Authorization"])
		if len(attempts) == 1 {
			return jsplugin.FetchResponse{Status: http.StatusTooManyRequests, Body: []byte("secret provider detail")}, nil
		}
		return jsplugin.FetchResponse{Status: http.StatusOK, Body: []byte(`{"results":[{"flagged":false}]}`)}, nil
	})
	request := jsplugin.FetchRequest{Method: http.MethodPost, URL: endpoint, Hook: "ingress", Body: `{"input":"[media:0]"}`, Headers: map[string]string{"authorization": "obsolete"},
		BodyStream: strings.NewReader(media), BodySize: int64(len(media)), ReplayBody: func() (io.Reader, int64, error) {
			return strings.NewReader(media), int64(len(media)), nil
		}}
	_, err := fetch(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bearer fixture-primary", "Bearer fixture-backup"}, attempts)
	pools := ctx.Value(isolatedModerationPoolsKey{}).(*moderationKeyPools)
	states := pools.pools[set.id].keys
	assert.Equal(t, http.StatusTooManyRequests, states[0].StatusCode)
	assert.Equal(t, "ok", states[1].Result)
	index, ok := pools.choose(set, states[0].CheckedAt)
	require.True(t, ok)
	assert.Equal(t, 1, index)
	index, ok = pools.choose(set, states[0].RetryAt)
	require.True(t, ok)
	assert.Equal(t, 0, index)
	encoded, err := common.Marshal(states)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "fixture-")
	assert.NotContains(t, string(encoded), "secret provider detail")
	_, other := ModerationKeyHealth(json.RawMessage(`{"url":"` + endpoint + `","apiKey":"fixture-primary","apiKeys":[{"key":"fixture-backup"}]}`))
	require.Len(t, other, 2)
	assert.Equal(t, "not_checked", other[0].Result)
}

func TestModerationPoolBoundsFailureAndCredentialDestination(t *testing.T) {
	const endpoint = "https://typesafe.example.invalid/v1/systemone"
	set := moderationKeys(json.RawMessage(`{"engine":"typesafe","typeSafeUrl":"` + endpoint + `","typeSafeApiKey":"jev-fixture","apiKey":"other-engine","auditTimeoutMs":1000,"auditRetryCount":3,"auditTotalTimeoutMs":500}`))
	require.NotNil(t, set)
	ctx := WithIsolatedModerationKeyPool(t.Context())
	var attempts int
	fetch := set.wrap(func(ctx context.Context, request jsplugin.FetchRequest) (jsplugin.FetchResponse, error) {
		attempts++
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		assert.Equal(t, "Bearer jev-fixture", request.Headers["Authorization"])
		assert.LessOrEqual(t, time.Until(deadline), 500*time.Millisecond)
		return jsplugin.FetchResponse{Status: http.StatusBadRequest, Body: []byte("private error detail")}, nil
	})
	request := jsplugin.FetchRequest{Method: http.MethodPost, URL: endpoint + "/other", Hook: "ingress"}
	_, err := fetch(ctx, request)
	require.ErrorContains(t, err, "configured audit endpoint")
	assert.Zero(t, attempts)
	request.URL = endpoint
	_, err = fetch(ctx, request)
	require.ErrorContains(t, err, "HTTP 400")
	assert.NotContains(t, err.Error(), "private error detail")
	assert.Equal(t, 1, attempts)
}
