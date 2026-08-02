package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type RetryParam struct {
	Ctx         *gin.Context
	TokenGroup  string
	ModelName   string
	RequestPath string
	Retry       *int
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

// CacheGetRandomSatisfiedChannel picks an eligible channel for the request.
// Auto groups are only traversed after an attempted channel when the token
// explicitly permits cross-group retries.
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)

	// Build the exclude list from channels already tried in this request.
	usedChannelStrs := param.Ctx.GetStringSlice("use_channel")
	excludeIDs := make([]int, 0, len(usedChannelStrs))
	for _, s := range usedChannelStrs {
		var id int
		if _, scanErr := fmt.Sscan(s, &id); scanErr == nil && id > 0 {
			excludeIDs = append(excludeIDs, id)
		}
	}

	if param.TokenGroup == "auto" {
		if len(setting.GetAutoGroups()) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		autoGroups := GetUserAutoGroup(userGroup)
		crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		startGroupIndex := 0
		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			// Always pass the full excludeIDs so a channel already tried in any
			// group is never reused, even if it belongs to multiple groups.
			// Channels exclusive to this group are not in excludeIDs and will be
			// selected normally.
			logger.LogDebug(param.Ctx, "Auto selecting group: %s, excluded: %v", autoGroup, excludeIDs)

			channel, _ = model.GetRandomSatisfiedChannel(autoGroup, param.ModelName, excludeIDs, param.RequestPath)
			if channel == nil {
				if len(excludeIDs) > 0 && !crossGroupRetry {
					return nil, selectGroup, nil
				}
				// Current group fully exhausted — move to next group.
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s, trying next group", autoGroup, param.ModelName)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				param.SetRetry(0)
				continue
			}
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			logger.LogDebug(param.Ctx, "Auto selected group: %s, channel: %d", autoGroup, channel.Id)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			break
		}
	} else {
		channel, err = model.GetRandomSatisfiedChannel(param.TokenGroup, param.ModelName, excludeIDs, param.RequestPath)
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	return channel, selectGroup, nil
}
