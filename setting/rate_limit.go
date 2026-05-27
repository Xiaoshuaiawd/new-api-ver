package setting

import (
	"fmt"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitMutex sync.RWMutex

type OpenAIUpstreamKeyLimitConfig struct {
	RPM         int    `json:"rpm"`
	TPM         int    `json:"tpm"`
	RPD         int    `json:"rpd"`
	TPD         int    `json:"tpd"`
	DailyWindow string `json:"daily_window"`
}

var OpenAIUpstreamKeyLimitEnabled = true
var OpenAIUpstreamKeyLimitConfigValue = DefaultOpenAIUpstreamKeyLimitConfig()

func DefaultOpenAIUpstreamKeyLimitConfig() OpenAIUpstreamKeyLimitConfig {
	return OpenAIUpstreamKeyLimitConfig{
		RPM:         3,
		TPM:         50000,
		RPD:         50,
		TPD:         200000,
		DailyWindow: "rolling_24h",
	}
}

func OpenAIUpstreamKeyLimitConfig2JSONString() string {
	jsonBytes, err := common.Marshal(OpenAIUpstreamKeyLimitConfigValue)
	if err != nil {
		common.SysLog("error marshalling OpenAI upstream key limit config: " + err.Error())
		return ""
	}
	return string(jsonBytes)
}

func UpdateOpenAIUpstreamKeyLimitConfigByJSONString(jsonStr string) error {
	if jsonStr == "" {
		OpenAIUpstreamKeyLimitConfigValue = DefaultOpenAIUpstreamKeyLimitConfig()
		return nil
	}
	var config OpenAIUpstreamKeyLimitConfig
	if err := common.UnmarshalJsonStr(jsonStr, &config); err != nil {
		return err
	}
	if err := CheckOpenAIUpstreamKeyLimitConfig(config); err != nil {
		return err
	}
	OpenAIUpstreamKeyLimitConfigValue = config
	return nil
}

func CheckOpenAIUpstreamKeyLimitConfig(config OpenAIUpstreamKeyLimitConfig) error {
	if config.RPM < 0 || config.TPM < 0 || config.RPD < 0 || config.TPD < 0 {
		return fmt.Errorf("OpenAI upstream key limits cannot be negative")
	}
	if config.DailyWindow == "" {
		return fmt.Errorf("daily_window is required")
	}
	if config.DailyWindow != "rolling_24h" {
		return fmt.Errorf("unsupported daily_window: %s", config.DailyWindow)
	}
	return nil
}

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	ModelRequestRateLimitGroup = make(map[string][2]int)
	return common.UnmarshalJsonStr(jsonStr, &ModelRequestRateLimitGroup)
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	checkModelRequestRateLimitGroup := make(map[string][2]int)
	err := common.UnmarshalJsonStr(jsonStr, &checkModelRequestRateLimitGroup)
	if err != nil {
		return err
	}
	for group, limits := range checkModelRequestRateLimitGroup {
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
	}

	return nil
}
