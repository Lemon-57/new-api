package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// GetIngressBilling shows the effective token-line factor for this API key.
// Individual models, task prices and media prices retain their own policies.
func GetIngressBilling(c *gin.Context) {
	group := c.GetString(string(constant.ContextKeyUsingGroup))
	userGroup := c.GetString(string(constant.ContextKeyUserGroup))
	if group == "auto" {
		c.JSON(http.StatusOK, gin.H{
			"billing_scope":         "token",
			"billing_route":         middleware.IngressBillingRoute(c),
			"route_rate_multiplier": middleware.IngressBillingFactor(c),
			"base_group_ratio":      nil,
			"effective_group_ratio": nil,
			"group":                 group,
		})
		return
	}
	base, special := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if !special {
		base = ratio_setting.GetGroupRatio(group)
	}
	route := middleware.IngressBillingRoute(c)
	if route == "" {
		route = "default"
	}
	c.JSON(http.StatusOK, gin.H{
		"billing_scope":         "token",
		"billing_route":         route,
		"route_rate_multiplier": middleware.IngressBillingFactor(c),
		"base_group_ratio":      base,
		"effective_group_ratio": base * middleware.IngressBillingFactor(c),
		"group":                 group,
	})
}
