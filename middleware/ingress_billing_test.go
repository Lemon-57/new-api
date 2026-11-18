package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIngressBillingRequiresTrustedProxyAndOneValidRoute(t *testing.T) {
	t.Setenv("NEWAPI_INGRESS_BILLING_ENABLED", "true")
	t.Setenv("NEWAPI_INGRESS_BILLING_SECRET", "a-private-proxy-secret-with-at-least-32-bytes")
	t.Setenv("NEWAPI_INGRESS_BILLING_FACTORS_JSON", `{"ordinary":1,"optimized":1.1}`)
	t.Setenv("NEWAPI_INGRESS_BILLING_TRUSTED_PROXY_CIDRS", "127.0.0.1/32,::1/128")
	middleware, err := IngressBilling()
	require.NoError(t, err)
	engine := gin.New()
	engine.Use(middleware)
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"route": IngressBillingRoute(c), "factor": IngressBillingFactor(c)})
	})
	engine.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := []struct {
		name     string
		peer     string
		routes   []string
		proofs   []string
		status   int
		response string
	}{
		{"ordinary", "127.0.0.1:34567", []string{"ordinary"}, []string{"a-private-proxy-secret-with-at-least-32-bytes"}, 200, `"factor":1`},
		{"optimized", "[::1]:34567", []string{"optimized"}, []string{"a-private-proxy-secret-with-at-least-32-bytes"}, 200, `"factor":1.1`},
		{"direct remote client", "192.0.2.2:34567", []string{"ordinary"}, []string{"a-private-proxy-secret-with-at-least-32-bytes"}, 403, "untrusted"},
		{"forged proof", "127.0.0.1:34567", []string{"ordinary"}, []string{"attacker"}, 403, "untrusted"},
		{"missing proof", "127.0.0.1:34567", []string{"ordinary"}, nil, 403, "untrusted"},
		{"missing route", "127.0.0.1:34567", nil, []string{"a-private-proxy-secret-with-at-least-32-bytes"}, 403, "untrusted"},
		{"duplicate route", "127.0.0.1:34567", []string{"ordinary", "optimized"}, []string{"a-private-proxy-secret-with-at-least-32-bytes"}, 403, "untrusted"},
		{"unknown route", "127.0.0.1:34567", []string{"cheaper"}, []string{"a-private-proxy-secret-with-at-least-32-bytes"}, 403, "unknown"},
		{"forwarded client IP does not grant trust", "192.0.2.2:34567", []string{"ordinary"}, []string{"a-private-proxy-secret-with-at-least-32-bytes"}, 403, "untrusted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			request.RemoteAddr = tc.peer
			request.Header.Set("X-Forwarded-For", "127.0.0.1")
			for _, value := range tc.routes {
				request.Header.Add(ingressRouteHeader, value)
			}
			for _, value := range tc.proofs {
				request.Header.Add(ingressProofHeader, value)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
			require.Contains(t, response.Body.String(), tc.response)
		})
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, response.Code)
}

func TestIngressBillingRejectsIncompleteConfiguration(t *testing.T) {
	t.Setenv("NEWAPI_INGRESS_BILLING_ENABLED", "true")
	t.Setenv("NEWAPI_INGRESS_BILLING_SECRET", "too-short")
	t.Setenv("NEWAPI_INGRESS_BILLING_FACTORS_JSON", `{"ordinary":1,"optimized":1.1}`)
	t.Setenv("NEWAPI_INGRESS_BILLING_TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	_, err := IngressBilling()
	require.Error(t, err)
	t.Setenv("NEWAPI_INGRESS_BILLING_SECRET", "a-private-proxy-secret-with-at-least-32-bytes")
	t.Setenv("NEWAPI_INGRESS_BILLING_FACTORS_JSON", `{"ordinary":1,"optimized":0.1}`)
	_, err = IngressBilling()
	require.Error(t, err)
	t.Setenv("NEWAPI_INGRESS_BILLING_FACTORS_JSON", `{"ordinary":1,"optimized":1.1}`)
	t.Setenv("NEWAPI_INGRESS_BILLING_TRUSTED_PROXY_CIDRS", "not-a-network")
	_, err = IngressBilling()
	require.Error(t, err)
}
