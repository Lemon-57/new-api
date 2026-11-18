package middleware

import (
	"crypto/subtle"
	"errors"
	"math"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	ingressRouteHeader = "X-NewAPI-Billing-Route"
	ingressProofHeader = "X-NewAPI-Billing-Proof"
	ingressRouteKey    = "ingress_billing_route"
	ingressFactorKey   = "ingress_billing_factor"
)

type ingressBillingConfig struct {
	secret string
	rates  map[string]float64
	proxy  []*net.IPNet
}

// IngressBilling reads the socket peer and headers supplied by the last
// trusted reverse proxy. The client-facing proxy must overwrite both headers.
// It is disabled until an operator has staged the proxy and its secret.
func IngressBilling() (gin.HandlerFunc, error) {
	if os.Getenv("NEWAPI_INGRESS_BILLING_ENABLED") != "true" {
		return func(c *gin.Context) { c.Next() }, nil
	}
	config, err := loadIngressBillingConfig()
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		if !isIngressBillablePath(c.Request.URL.Path) {
			c.Next()
			return
		}
		peer, _, splitErr := net.SplitHostPort(c.Request.RemoteAddr)
		ip := net.ParseIP(peer)
		trusted := splitErr == nil && ip != nil
		if trusted {
			trusted = false
			for _, network := range config.proxy {
				if network.Contains(ip) {
					trusted = true
					break
				}
			}
		}
		routes := c.Request.Header.Values(ingressRouteHeader)
		proofs := c.Request.Header.Values(ingressProofHeader)
		if !trusted || len(routes) != 1 || len(proofs) != 1 ||
			subtle.ConstantTimeCompare([]byte(proofs[0]), []byte(config.secret)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "untrusted billing ingress"})
			return
		}
		route := strings.TrimSpace(routes[0])
		factor, ok := config.rates[route]
		if !ok || route != routes[0] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "unknown billing ingress"})
			return
		}
		c.Set(ingressRouteKey, route)
		c.Set(ingressFactorKey, factor)
		c.Next()
	}, nil
}

func loadIngressBillingConfig() (*ingressBillingConfig, error) {
	secret := os.Getenv("NEWAPI_INGRESS_BILLING_SECRET")
	if len(secret) < 32 || strings.TrimSpace(secret) != secret {
		return nil, errors.New("invalid ingress billing secret")
	}
	var rates map[string]float64
	if err := common.Unmarshal([]byte(os.Getenv("NEWAPI_INGRESS_BILLING_FACTORS_JSON")), &rates); err != nil {
		return nil, err
	}
	if len(rates) != 2 || rates["ordinary"] != 1 || rates["optimized"] < 1 ||
		math.IsNaN(rates["optimized"]) || math.IsInf(rates["optimized"], 0) {
		return nil, errors.New("invalid ingress billing factors")
	}
	var trusted []*net.IPNet
	for cidr := range strings.SplitSeq(os.Getenv("NEWAPI_INGRESS_BILLING_TRUSTED_PROXY_CIDRS"), ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			return nil, errors.New("invalid ingress billing proxy CIDR")
		}
		trusted = append(trusted, network)
	}
	if len(trusted) == 0 {
		return nil, errors.New("missing ingress billing proxy CIDRs")
	}
	return &ingressBillingConfig{secret: secret, rates: rates, proxy: trusted}, nil
}

func isIngressBillablePath(path string) bool {
	_, tail, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	return path == "/v1" || strings.HasPrefix(path, "/v1/") ||
		path == "/v1beta" || strings.HasPrefix(path, "/v1beta/") ||
		path == "/mj" || strings.HasPrefix(path, "/mj/") ||
		path == "/pg" || strings.HasPrefix(path, "/pg/") ||
		tail == "mj" || strings.HasPrefix(tail, "mj/")
}

// IngressBillingFactor returns the frozen request's rate adjustment. The
// absence of a route means an internal admin operation or disabled feature.
func IngressBillingFactor(c *gin.Context) float64 {
	if factor, ok := c.Get(ingressFactorKey); ok {
		if value, ok := factor.(float64); ok {
			return value
		}
	}
	return 1
}

func IngressBillingRoute(c *gin.Context) string {
	return c.GetString(ingressRouteKey)
}
