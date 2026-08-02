package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/promptguard"
	prompt_guard_setting "github.com/QuantumNous/new-api/setting/prompt_guard_setting"
	"github.com/gin-gonic/gin"
)

// GetPromptGuardConfig returns the current Prompt Guard config (tokens redacted).
func GetPromptGuardConfig(c *gin.Context) {
	pub := prompt_guard_setting.GetPublic()
	common.ApiSuccess(c, pub)
}

// UpdatePromptGuardConfig validates and saves a new Prompt Guard config.
func UpdatePromptGuardConfig(c *gin.Context) {
	var req prompt_guard_setting.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误: "+err.Error())
		return
	}

	if err := validatePromptGuardUpdate(req); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	// Load current config and check optimistic-lock version
	current := prompt_guard_setting.GetPublic()
	if req.ExpectedVersion != 0 && req.ExpectedVersion != current.ConfigVersion {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "配置版本冲突，请刷新后重试",
		})
		return
	}

	// Build new storage config, preserving existing token ciphertexts when no new token is provided
	newCfg := buildStorageConfig(req, current)

	// Persist as JSON in the option table
	b, err := common.Marshal(newCfg)
	if err != nil {
		common.ApiErrorMsg(c, "序列化配置失败")
		return
	}
	if err := model.UpdateOption("prompt_guard_setting", string(b)); err != nil {
		logger.LogError(c, "failed to save prompt_guard_setting: "+err.Error())
		common.ApiErrorMsg(c, "保存配置失败")
		return
	}

	common.ApiSuccess(c, prompt_guard_setting.GetPublic())
}

// ProbePromptGuardEndpoint sends a test classification request to a guard endpoint.
func ProbePromptGuardEndpoint(c *gin.Context) {
	var body struct {
		BaseURL   string `json:"base_url"`
		Model     string `json:"model"`
		Token     string `json:"token"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if strings.TrimSpace(body.BaseURL) == "" {
		common.ApiErrorMsg(c, "base_url 不能为空")
		return
	}

	ep := promptguard.Endpoint{
		ID:        "probe",
		BaseURL:   body.BaseURL,
		Model:     body.Model,
		Token:     body.Token,
		TimeoutMS: body.TimeoutMS,
		Enabled:   true,
	}
	if ep.TimeoutMS <= 0 {
		ep.TimeoutMS = promptguard.DefaultTimeoutMS
	}

	cfg := promptguard.Config{
		Enabled:         true,
		BlockingEnabled: true,
		Scanners:        promptguard.AllScannerIDs,
		AllGroups:       true,
		Endpoints:       []promptguard.Endpoint{ep},
	}

	snap := promptguard.BuildSnapshot(
		"Hello, how are you?",
		"Hello, how are you?",
		"",
		false,
		ep.InputLimit,
		"probe",
		"probe",
		"probe",
	)

	decision, err := promptguard.Evaluate(c.Request.Context(), cfg, snap)
	if err != nil {
		common.ApiErrorMsg(c, "节点探测失败: "+err.Error())
		return
	}

	common.ApiSuccess(c, gin.H{
		"decision": decision.Kind,
		"latency_ms": decision.LatencyMS,
	})
}

// validatePromptGuardUpdate checks the update request semantics.
func validatePromptGuardUpdate(req prompt_guard_setting.UpdateRequest) error {
	if req.BlockingEnabled && !req.Enabled {
		return fmt.Errorf("开启同步阻断前必须先启用 Prompt Guard")
	}
	if req.Enabled {
		if !req.AllGroups && len(req.GroupNames) == 0 {
			return fmt.Errorf("启用时必须选择至少一个分组或选择全部分组")
		}
		if len(req.Scanners) == 0 {
			return fmt.Errorf("启用时必须选择至少一个风险分类")
		}
		enabledCount := 0
		for _, ep := range req.Endpoints {
			if ep.Enabled {
				enabledCount++
			}
		}
		if enabledCount == 0 {
			return fmt.Errorf("启用时必须有至少一个启用的 Guard 节点")
		}
	}
	for _, s := range req.Scanners {
		if !promptguard.KnownScannerID(normalizeScanner(s)) {
			return fmt.Errorf("未知的风险分类: %s", s)
		}
	}
	return nil
}

func normalizeScanner(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, " ", "_")))
}

// buildStorageConfig merges the update request with the current persisted config.
// Token fields: empty string keeps old cipher; non-empty replaces it (plaintext, same pattern as SMTPToken).
func buildStorageConfig(req prompt_guard_setting.UpdateRequest, current prompt_guard_setting.PublicConfig) prompt_guard_setting.StorageConfig {
	// Map current ciphertexts by endpoint ID
	existingTokens := make(map[string]string)
	for _, ep := range current.Endpoints {
		// We need the cipher from the raw stored config, not the public view.
		// Re-read via GetPublic which gives has_token only; raw token preserved via GetStorageCopy.
		_ = ep
	}
	// Re-read raw config to preserve existing tokens
	rawJSON, _ := prompt_guard_setting.GetStorageJSON()
	var existing prompt_guard_setting.StorageConfig
	_ = common.UnmarshalJsonStr(rawJSON, &existing)
	for _, ep := range existing.Endpoints {
		if ep.TokenCipher != "" {
			existingTokens[ep.ID] = ep.TokenCipher
		}
	}

	scanners := req.Scanners
	if len(scanners) == 0 {
		scanners = promptguard.AllScannerIDs
	}
	groups := req.GroupNames
	if groups == nil {
		groups = []string{}
	}

	eps := make([]prompt_guard_setting.StorageEndpoint, 0, len(req.Endpoints))
	for _, ep := range req.Endpoints {
		tokenCipher := existingTokens[ep.ID]
		if ep.ClearToken {
			tokenCipher = ""
		} else if strings.TrimSpace(ep.Token) != "" {
			// plaintext — stored directly (same pattern as SMTPToken)
			tokenCipher = strings.TrimSpace(ep.Token)
		}
		timeoutMS := ep.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = promptguard.DefaultTimeoutMS
		}
		inputLimit := ep.InputLimit
		if inputLimit <= 0 {
			inputLimit = promptguard.DefaultInputLimit
		}
		eps = append(eps, prompt_guard_setting.StorageEndpoint{
			ID:          ep.ID,
			Name:        ep.Name,
			BaseURL:     ep.BaseURL,
			Model:       ep.Model,
			TokenCipher: tokenCipher,
			TimeoutMS:   timeoutMS,
			InputLimit:  inputLimit,
			Enabled:     ep.Enabled,
		})
	}

	return prompt_guard_setting.StorageConfig{
		Enabled:         req.Enabled,
		BlockingEnabled: req.BlockingEnabled,
		LatestTurnOnly:  req.LatestTurnOnly,
		StorePassEvents: req.StorePassEvents,
		Scanners:        scanners,
		AllGroups:       req.AllGroups,
		GroupNames:      groups,
		Endpoints:       eps,
		ConfigVersion:   current.ConfigVersion + 1,
	}
}
