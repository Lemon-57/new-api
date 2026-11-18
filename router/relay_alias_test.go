package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVersionlessRelayAliasUsesGatewayAuthentication(t *testing.T) {
	engine := gin.New()
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	server := WithRelayAliases(engine)

	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/models"},
		{http.MethodPost, "/responses"},
		{http.MethodPost, "/responses/compact"},
		{http.MethodPost, "/chat/completions"},
		{http.MethodPost, "/messages"},
		{http.MethodPost, "/images/generations"},
	} {
		for _, prefix := range []string{"", "/v1"} {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(test.method, prefix+test.path, strings.NewReader(`{}`)))
			require.Equal(t, http.StatusUnauthorized, response.Code, "%s%s", prefix, test.path)
			require.Contains(t, response.Header().Get("Content-Type"), "application/json")
		}
	}
}

func TestVersionlessRelayAliasPreservesRequestAndOtherRoutes(t *testing.T) {
	engine := gin.New()
	engine.GET("/v1/models/:model", func(ctx *gin.Context) { ctx.String(http.StatusOK, "%s", ctx.Request.RequestURI) })
	engine.NoRoute(func(ctx *gin.Context) { ctx.String(http.StatusOK, "%s", ctx.Request.RequestURI) })
	server := WithRelayAliases(engine)

	for _, test := range []struct{ incoming, expected string }{
		{"/models/model%20name?filter=a%2Fb", "/v1/models/model%20name?filter=a%2Fb"},
		{"/v1/models/model%20name?filter=a%2Fb", "/v1/models/model%20name?filter=a%2Fb"},
		{"/v1beta/models", "/v1beta/models"},
		{"/api/user/self", "/api/user/self"},
		{"/models-page", "/models-page"},
		{"/wallet", "/wallet"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.incoming, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		require.Equal(t, test.expected, response.Body.String())
		require.Equal(t, test.incoming, request.RequestURI)
	}
}
