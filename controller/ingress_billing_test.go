package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIngressBillingInquiryUsesTokenGroupAndTrustedRoute(t *testing.T) {
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio": `{"normal":0.2}`,
	}))
	for _, tc := range []struct {
		route  string
		factor float64
		want   float64
	}{
		{"ordinary", 1, 0.2},
		{"optimized", 1.1, 0.22},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/billing", nil)
		ctx.Set("group", "normal")
		ctx.Set("user_group", "normal")
		ctx.Set("ingress_billing_route", tc.route)
		ctx.Set("ingress_billing_factor", tc.factor)
		GetIngressBilling(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		require.Equal(t, tc.route, body["billing_route"])
		require.InDelta(t, tc.want, body["effective_group_ratio"], 0.0000001)
	}
}
