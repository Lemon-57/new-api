package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCutoverDrainsExistingResponseAndRoutesQueuedRequestsToFinalBackend(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "ordinary", r.Header.Get("X-NewAPI-Billing-Route"))
		require.Equal(t, "private-test-proof-at-least-thirty-two-bytes", r.Header.Get("X-NewAPI-Billing-Proof"))
		close(started)
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "last")
	}))
	defer old.Close()
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "ordinary", r.Header.Get("X-Sub2API-Billing-Route"))
		require.Empty(t, r.Header.Get("X-NewAPI-Billing-Proof"))
		_, _ = io.WriteString(w, "final")
	}))
	defer next.Close()
	g, err := newGateway(config{NewAPI: old.URL, Sub2API: next.URL, Images: next.URL, Proof: "private-test-proof-at-least-thirty-two-bytes", State: filepath.Join(t.TempDir(), "state")})
	require.NoError(t, err)
	handler := g.handler(listener{Route: "ordinary"})
	req := httptest.NewRequest(http.MethodGet, "http://newapi.killai.cc/v1/responses", nil)
	req.Header.Set("X-NewAPI-Billing-Route", "optimized")
	req.Header.Set("X-NewAPI-Billing-Proof", "client-forgery")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); handler.ServeHTTP(response, req) }()
	<-started
	paused := httptest.NewRecorder()
	g.control(paused, httptest.NewRequest(http.MethodPost, "/pause", nil))
	require.Equal(t, http.StatusOK, paused.Code)
	rejected := httptest.NewRecorder()
	g.control(rejected, httptest.NewRequest(http.MethodPost, "/swap", nil))
	require.Equal(t, http.StatusConflict, rejected.Code)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, httptest.NewRequest(http.MethodPost, "http://newapi.killai.cc/v1/responses", nil).WithContext(ctx))
	require.Empty(t, cancelled.Body.String(), "a cancelled queued request must never be forwarded")
	close(release)
	<-done
	require.Equal(t, "firstlast", response.Body.String(), "an active stream must finish intact")
	committed := httptest.NewRecorder()
	g.control(committed, httptest.NewRequest(http.MethodPost, "/swap", nil))
	require.Equal(t, http.StatusOK, committed.Code)
	final := httptest.NewRecorder()
	handler.ServeHTTP(final, httptest.NewRequest(http.MethodPost, "http://newapi.killai.cc/v1/responses", strings.NewReader("{}")))
	require.Equal(t, "final", final.Body.String())
	reloaded, err := newGateway(g.config)
	require.NoError(t, err)
	require.Equal(t, "final", reloaded.mode)
}

func TestPrivatePeerRequiresTrustedRouteAndRetainsItsServiceAcrossDomainSwap(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "optimized", r.Header.Get("X-Sub2API-Billing-Route"))
		_, _ = io.WriteString(w, "source")
	}))
	defer upstream.Close()
	g, err := newGateway(config{NewAPI: upstream.URL, Sub2API: upstream.URL, Images: upstream.URL, State: filepath.Join(t.TempDir(), "state")})
	require.NoError(t, err)
	g.mode = "final"
	h := g.handler(listener{Route: "peer", Service: "sub2api"})
	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "http://internal/v1/responses", nil))
	require.Equal(t, http.StatusForbidden, bad.Code)
	r := httptest.NewRequest(http.MethodPost, "http://internal/v1/responses", nil)
	r.Header.Set("X-Sub2API-Billing-Route", "optimized")
	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, r)
	require.Equal(t, "source", ok.Body.String())
}
