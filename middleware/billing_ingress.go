package middleware

import (
	"crypto/subtle"
	"errors"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

const BillingIngressHeader = "X-NewAPI-Billing-Ingress"
const BillingIngressTokenHeader = "X-NewAPI-Billing-Ingress-Token"

// NewBillingIngress is opt-in. Invalid attestations fail closed on relay
// endpoints. Credentials are erased before logging, snapshots or upstream relay.
func NewBillingIngress(routesJSON, token, trustedCIDRs string) (gin.HandlerFunc, error) {
	if routesJSON == "" && token == "" {
		return func(c *gin.Context) { c.Next() }, nil
	}
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("billing ingress requires a private token of at least 32 characters")
	}
	var routes map[string]common.BillingIngress
	if err := common.UnmarshalJsonStr(routesJSON, &routes); err != nil || len(routes) == 0 {
		return nil, errors.New("billing ingress routes must be a nonempty JSON object")
	}
	for name, route := range routes {
		if name == "" || len(name) > 64 || strings.ContainsAny(name, " \t\r\n,") {
			return nil, errors.New("billing ingress route name is invalid")
		}
		if route.GroupMultiplier <= 0 || math.IsNaN(route.GroupMultiplier) || math.IsInf(route.GroupMultiplier, 0) ||
			route.ImageMultiplier <= 0 || math.IsNaN(route.ImageMultiplier) || math.IsInf(route.ImageMultiplier, 0) {
			return nil, errors.New("billing ingress multipliers must be finite and positive")
		}
		route.Name = name
		for _, model := range route.ImageModels {
			if model == "" || len(model) > 256 || strings.ContainsAny(model, "\r\n") {
				return nil, errors.New("billing ingress image model is invalid")
			}
		}
		route.ImageModels = append([]string(nil), route.ImageModels...)
		routes[name] = route
	}
	if trustedCIDRs == "" {
		trustedCIDRs = "127.0.0.1/32,::1/128"
	}
	var peers []netip.Prefix
	for raw := range strings.SplitSeq(trustedCIDRs, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, errors.New("billing ingress trusted peer CIDR is invalid")
		}
		peers = append(peers, prefix)
	}
	return func(c *gin.Context) {
		names := c.Request.Header.Values(BillingIngressHeader)
		tokens := c.Request.Header.Values(BillingIngressTokenHeader)
		c.Request.Header.Del(BillingIngressHeader)
		c.Request.Header.Del(BillingIngressTokenHeader)
		validPeer := false
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err == nil {
			if address, err := netip.ParseAddr(host); err == nil {
				for _, prefix := range peers {
					if prefix.Contains(address.Unmap()) {
						validPeer = true
						break
					}
				}
			}
		}
		var route common.BillingIngress
		valid := false
		if validPeer && len(names) == 1 && len(tokens) == 1 &&
			subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(token)) == 1 {
			route, valid = routes[names[0]]
		}
		if valid {
			c.Request = c.Request.WithContext(common.WithBillingIngress(c.Request.Context(), route))
		} else {
			path := c.Request.URL.Path
			relay := path == "/v1" || strings.HasPrefix(path, "/v1/") ||
				strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/v1alpha/") ||
				strings.HasPrefix(path, "/pg/") || c.GetHeader("Upgrade") != "" ||
				(c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead &&
					c.Request.Method != http.MethodOptions && !strings.HasPrefix(path, "/api/"))
			if relay {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{
					"type": "invalid_request_error", "code": "untrusted_billing_ingress",
					"message": i18n.T(c, i18n.MsgBillingIngressRequired),
				}})
				return
			}
		}
		c.Next()
	}, nil
}
