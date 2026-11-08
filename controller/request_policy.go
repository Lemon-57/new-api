package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func GetRequestPolicy(c *gin.Context) {
	options := make(map[string]string, len(model.CurrentRequestPolicy().Options)+1)
	for key, value := range model.CurrentRequestPolicy().Options {
		if key == setting.ContentModerationAPIKeyKey {
			continue
		}
		options[key] = value
	}
	cfg := setting.GetContentModerationConfig()
	options[setting.ContentModerationKeyConfiguredKey] = strconv.FormatBool(len(cfg.APIKeys()) > 0)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"options": options, "moderation_keys": setting.ContentModerationKeyStatuses(cfg)}})
}

func UpdateRequestPolicy(c *gin.Context) {
	var request struct {
		Options map[string]string `json:"options"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request policy payload"})
		return
	}
	if err := model.UpdateRequestPolicyOptions(request.Options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	GetRequestPolicy(c)
}
