package router

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const relayAliasTestProof = "local-relay-alias-proof-for-tests-only"

func relayAliasTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	t.Setenv("NEWAPI_INGRESS_BILLING_ENABLED", "true")
	t.Setenv("NEWAPI_INGRESS_BILLING_SECRET", relayAliasTestProof)
	t.Setenv("NEWAPI_INGRESS_BILLING_FACTORS_JSON", `{"ordinary":1,"optimized":1.1}`)
	t.Setenv("NEWAPI_INGRESS_BILLING_TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	ingress, err := middleware.IngressBilling()
	require.NoError(t, err)
	engine := gin.New()
	engine.Use(ingress)
	engine.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer local-test-key" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "test authentication required"})
			return
		}
		c.Next()
	})
	engine.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "unknown API endpoint"})
			return
		}
		c.Data(http.StatusOK, "text/html", []byte("<html>console</html>"))
	})
	return engine
}

func TestRelayAliasesPreserveRequestAndProtection(t *testing.T) {
	engine := relayAliasTestEngine(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/v1/models"},
		{http.MethodGet, "/v1/models/:model"},
		{http.MethodPost, "/v1/responses"},
		{http.MethodPost, "/v1/responses/compact"},
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodPost, "/v1/messages"},
		{http.MethodPost, "/v1/embeddings"},
		{http.MethodPost, "/v1/images/generations"},
		{http.MethodPost, "/v1/images/edits"},
		{http.MethodGet, "/v1/videos/:task/content"},
		{http.MethodPost, "/v1/files/*path"},
		{http.MethodPost, "/v1/audio/transcriptions"},
	} {
		engine.Handle(route.method, route.path, func(c *gin.Context) {
			body, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			c.JSON(http.StatusOK, gin.H{
				"path": c.Request.URL.Path, "uri": c.Request.RequestURI,
				"rawPath": c.Request.URL.RawPath, "bodySHA256": fmt.Sprintf("%x", sha256.Sum256(body)),
				"contentType": c.GetHeader("Content-Type"),
				"route":       middleware.IngressBillingRoute(c),
				"factor":      middleware.IngressBillingFactor(c),
			})
		})
	}
	handler := WithRelayAliases(engine)
	for _, tt := range []struct{ method, path, body, contentType string }{
		{http.MethodGet, "/models?client_version=1&filter=a%2Fb", "", ""},
		{http.MethodGet, "/models/model%20name?key=local-test-query", "", ""},
		{http.MethodPost, "/responses?background=false", `{"model":"test","input":"hello","stream":false}`, "application/json"},
		{http.MethodPost, "/responses/compact", `{"model":"test","input":[]}`, "application/json"},
		{http.MethodPost, "/chat/completions", `{"model":"test","messages":[]}`, "application/json"},
		{http.MethodPost, "/messages", `{"model":"test","messages":[]}`, "application/json"},
		{http.MethodPost, "/embeddings", `{"model":"test","input":"hello"}`, "application/json"},
		{http.MethodPost, "/images/generations", `{"model":"test","prompt":"circle"}`, "application/json"},
		{http.MethodPost, "/images/edits", "--local-boundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"test.png\"\r\n\r\n\x00\x01\xff\r\n--local-boundary--\r\n", "multipart/form-data; boundary=local-boundary"},
		{http.MethodGet, "/videos/task-1/content?download=1", "", ""},
		{http.MethodPost, "/files/a/b%20c?purpose=test", "original upload bytes", "application/octet-stream"},
		{http.MethodPost, "/audio/transcriptions", "original audio bytes", "multipart/form-data; boundary=audio"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			var replies []string
			for _, target := range []string{tt.path, "/v1" + tt.path} {
				req := httptest.NewRequest(tt.method, target, strings.NewReader(tt.body))
				req.RemoteAddr = "127.0.0.1:12345"
				req.Header.Set("Authorization", "Bearer local-test-key")
				req.Header.Set("Content-Type", tt.contentType)
				req.Header.Set("X-NewAPI-Billing-Route", "optimized")
				req.Header.Set("X-NewAPI-Billing-Proof", relayAliasTestProof)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var got map[string]any
				require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &got))
				assert.Equal(t, 1.1, got["factor"])
				assert.Equal(t, "optimized", got["route"])
				assert.Equal(t, tt.contentType, got["contentType"])
				assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(tt.body))), got["bodySHA256"])
				assert.Equal(t, "/v1"+tt.path, got["uri"])
				assert.Equal(t, target, req.RequestURI, "normalization must not mutate the caller's request")
				replies = append(replies, rec.Body.String())
			}
			assert.Equal(t, replies[0], replies[1])
		})
	}

	for _, path := range []string{"/responses", "/v1/responses"} {
		for _, tt := range []struct {
			name, proof, authorization string
			status                     int
		}{
			{"untrusted ingress", "", "Bearer local-test-key", http.StatusForbidden},
			{"missing key", relayAliasTestProof, "", http.StatusUnauthorized},
			{"invalid key", relayAliasTestProof, "Bearer invalid-test-key", http.StatusUnauthorized},
		} {
			t.Run(path+"/"+tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
				req.RemoteAddr = "127.0.0.1:12345"
				req.Header.Set("Authorization", tt.authorization)
				req.Header.Set("X-NewAPI-Billing-Route", "ordinary")
				req.Header.Set("X-NewAPI-Billing-Proof", tt.proof)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				assert.Equal(t, tt.status, rec.Code)
				assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			})
		}
	}
}

func TestRelayAliasesKeepOtherNamespaces(t *testing.T) {
	engine := gin.New()
	engine.POST("/v1/responses", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, c.Request.RequestURI) })
	handler := WithRelayAliases(engine)
	for _, path := range []string{"/", "/wallet", "/pricing", "/api/user/self", "/assets/logo.svg", "/v1beta/models", "/models-page", "/responses-ui", "/chat/history"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			assert.Equal(t, path, rec.Body.String())
		})
	}
}

func TestRelayAliasesUseGatewayAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	handler := WithRelayAliases(engine)
	for _, path := range []string{"/models", "/responses", "/responses/compact", "/chat/completions", "/messages", "/images/generations", "/images/edits"} {
		t.Run(path, func(t *testing.T) {
			method := http.MethodPost
			if path == "/models" {
				method = http.MethodGet
			}
			for _, target := range []string{path, "/v1" + path} {
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(`{}`)))
				assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
				assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			}
		})
	}
}

func TestRelayAliasesStreamAndUpgrade(t *testing.T) {
	engine := relayAliasTestEngine(t)
	flushed := make(chan chan struct{}, 1)
	engine.POST("/v1/responses", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = io.WriteString(c.Writer, "data: {\"type\":\"response.created\"}\n\n")
		c.Writer.Flush()
		finish := make(chan struct{})
		flushed <- finish
		select {
		case <-finish:
		case <-c.Request.Context().Done():
			return
		}
		_, _ = io.WriteString(c.Writer, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	})
	engine.GET("/v1/responses", func(c *gin.Context) {
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer connection.Close()
		_, body, err := connection.ReadMessage()
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, `{"type":"response.create"}`, string(body))
		assert.NoError(t, connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed"}`)))
	})
	server := httptest.NewServer(WithRelayAliases(engine))
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/responses", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			headers := http.Header{
				"Authorization":          {"Bearer local-test-key"},
				"X-Newapi-Billing-Route": {"ordinary"},
				"X-Newapi-Billing-Proof": {relayAliasTestProof},
			}
			req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(`{"stream":true}`))
			require.NoError(t, err)
			req.Header = headers.Clone()
			response, err := client.Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode)
			reader := bufio.NewReader(response.Body)
			line, err := reader.ReadString('\n')
			require.NoError(t, err)
			assert.Equal(t, "data: {\"type\":\"response.created\"}\n", line)
			close(<-flushed)
			tail, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Contains(t, string(tail), `"type":"response.completed"`)
			require.NoError(t, response.Body.Close())

			connection, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path, headers)
			require.NoError(t, err)
			require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
			require.NoError(t, connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)))
			_, body, err := connection.ReadMessage()
			require.NoError(t, err)
			assert.Equal(t, `{"type":"response.completed"}`, string(body))
			require.NoError(t, connection.Close())
		})
	}
}
