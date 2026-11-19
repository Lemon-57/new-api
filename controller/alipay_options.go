package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/smartwalle/alipay/v3"
	"github.com/smartwalle/nsign"
)

type alipayVerifier struct {
	AppID     string `json:"app_id"`
	PublicKey string `json:"public_key"`
}

func (verifier alipayVerifier) id() string {
	digest := sha256.Sum256([]byte(verifier.AppID + "\x00" + verifier.PublicKey))
	return hex.EncodeToString(digest[:16])
}

func (verifier alipayVerifier) decoder() (*alipay.Client, error) {
	if verifier.AppID == "" || verifier.PublicKey == "" {
		return nil, errors.New("missing Alipay verifier")
	}
	client, err := alipay.NewWithSigner(verifier.AppID, nsign.New(), false)
	if err != nil {
		return nil, err
	}
	if err := client.LoadAliPayPublicKey(verifier.PublicKey); err != nil {
		return nil, err
	}
	return client, nil
}

type alipayVerifierRegistry struct {
	Legacy  *alipayVerifier           `json:"legacy,omitempty"`
	Retired map[string]alipayVerifier `json:"retired,omitempty"`
}

func readAlipayVerifierRegistry(options map[string]string) (alipayVerifierRegistry, error) {
	registry := alipayVerifierRegistry{}
	if encoded := options[model.AlipayVerifierRegistryOption]; encoded != "" {
		if err := common.UnmarshalJsonStr(encoded, &registry); err != nil {
			return registry, err
		}
	}
	return registry, nil
}

func alipayVerifierForOrder(options map[string]string, tradeNo string) (alipayVerifier, error) {
	current := directAlipaySettingsFromOptions(options).verifier()
	registry, err := readAlipayVerifierRegistry(options)
	if err != nil {
		return alipayVerifier{}, err
	}
	_, id, versioned := strings.Cut(tradeNo, "_")
	if !versioned {
		if registry.Legacy != nil {
			return *registry.Legacy, nil
		}
		return current, nil
	}
	if current.id() == id {
		return current, nil
	}
	if verifier, ok := registry.Retired[id]; ok && verifier.id() == id {
		return verifier, nil
	}
	return alipayVerifier{}, errors.New("unknown Alipay order verifier")
}

type alipayOptionsView struct {
	AppID                string `json:"app_id"`
	PublicKey            string `json:"public_key"`
	PrivateKeyConfigured bool   `json:"private_key_configured"`
	NotifyURL            string `json:"notify_url"`
	ReturnURL            string `json:"return_url"`
	Revision             string `json:"revision"`
}

func directAlipayOptionsView(options map[string]string) alipayOptionsView {
	settings := directAlipaySettingsFromOptions(options)
	encoded, _ := common.Marshal(options)
	revision := sha256.Sum256(encoded)
	return alipayOptionsView{
		AppID: settings.appID, PublicKey: settings.publicKey, PrivateKeyConfigured: settings.privateKey != "",
		NotifyURL: settings.notifyURL, ReturnURL: settings.returnURL, Revision: hex.EncodeToString(revision[:]),
	}
}

func GetDirectAlipaySettings(c *gin.Context) {
	options, err := model.GetDirectAlipayOptions()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Please try again later."))
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, directAlipayOptionsView(options))
}

func SaveDirectAlipaySettings(c *gin.Context) {
	var request struct {
		AppID      *string `json:"app_id"`
		PublicKey  *string `json:"public_key"`
		PrivateKey string  `json:"private_key"`
		NotifyURL  *string `json:"notify_url"`
		ReturnURL  *string `json:"return_url"`
		Revision   string  `json:"revision"`
	}
	if c.ContentType() != "application/json" || c.ShouldBindJSON(&request) != nil || request.AppID == nil || request.PublicKey == nil || request.NotifyURL == nil || request.ReturnURL == nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	options, err := model.GetDirectAlipayOptions()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Please try again later."))
		return
	}
	if request.Revision == "" || request.Revision != directAlipayOptionsView(options).Revision {
		common.ApiErrorStatus(c, http.StatusConflict, common.NewMessage("Payment settings changed. Reload and try again."))
		return
	}
	previous := directAlipaySettingsFromOptions(options)
	next := directAlipaySettings{
		appID: strings.TrimSpace(*request.AppID), privateKey: strings.TrimSpace(request.PrivateKey),
		publicKey: strings.TrimSpace(*request.PublicKey), notifyURL: strings.TrimSpace(*request.NotifyURL),
		returnURL: strings.TrimSpace(*request.ReturnURL),
	}
	if next.privateKey == "" {
		next.privateKey = previous.privateKey
	}
	if next.appID != previous.appID && previous.appID != "" && strings.TrimSpace(request.PrivateKey) == "" {
		common.ApiErrorT(c, "Enter the private key for the new Alipay App ID.")
		return
	}
	for _, addresses := range [][2]string{{previous.notifyURL, next.notifyURL}, {previous.returnURL, next.returnURL}} {
		if addresses[0] == addresses[1] || addresses[1] == "" {
			continue
		}
		parsed, parseErr := url.Parse(addresses[1])
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
			common.ApiErrorT(c, "Enter a valid HTTPS URL.")
			return
		}
	}
	if _, err := next.client(); err != nil {
		common.ApiErrorT(c, "Enter a valid Alipay App ID and RSA keys.")
		return
	}
	registry, err := readAlipayVerifierRegistry(options)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Please try again later."))
		return
	}
	if verifier := previous.verifier(); verifier != next.verifier() && verifier.AppID != "" && verifier.PublicKey != "" {
		if registry.Legacy == nil {
			registry.Legacy = &verifier
		}
		if registry.Retired == nil {
			registry.Retired = make(map[string]alipayVerifier)
		}
		registry.Retired[verifier.id()] = verifier
	}
	encoded, err := common.Marshal(registry)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Please try again later."))
		return
	}
	updated := map[string]string{
		"AlipayAppID": next.appID, "AlipayPrivateKey": next.privateKey, "AlipayPublicKey": next.publicKey,
		"AlipayNotifyURL": next.notifyURL, "AlipayReturnURL": next.returnURL, model.AlipayVerifierRegistryOption: string(encoded),
	}
	if err := model.ReplaceDirectAlipayOptions(options, updated); err != nil {
		if errors.Is(err, model.ErrAlipayOptionsChanged) {
			common.ApiErrorStatus(c, http.StatusConflict, common.NewMessage("Payment settings changed. Reload and try again."))
			return
		}
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Please try again later."))
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, directAlipayOptionsView(updated))
}
