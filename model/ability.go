package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where(commonGroupCol+" = ? and enabled = ?", group, true).Distinct("model").Pluck("model", &models)
	return models
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func getPriority(group string, model string, retry int) (int, error) {

	var priorities []int
	err := DB.Model(&Ability{}).
		Select("DISTINCT(priority)").
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
		Order("priority DESC").              // 按优先级降序排序
		Pluck("priority", &priorities).Error // Pluck用于将查询的结果直接扫描到一个切片中

	if err != nil {
		// 处理错误
		return 0, err
	}

	if len(priorities) == 0 {
		// 如果没有查询到优先级，则返回错误
		return 0, errors.New("数据库一致性被破坏")
	}

	// 确定要使用的优先级
	var priorityToUse int
	if retry >= len(priorities) {
		// 如果重试次数大于优先级数，则使用最小的优先级
		priorityToUse = priorities[len(priorities)-1]
	} else {
		priorityToUse = priorities[retry]
	}
	return priorityToUse, nil
}

func getChannelQuery(group string, model string, retry int) (*gorm.DB, error) {
	maxPrioritySubQuery := DB.Model(&Ability{}).Select("MAX(priority)").Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true)
	channelQuery := DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = (?)", group, model, true, maxPrioritySubQuery)
	if retry != 0 {
		priority, err := getPriority(group, model, retry)
		if err != nil {
			return nil, err
		} else {
			channelQuery = DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = ?", group, model, true, priority)
		}
	}

	return channelQuery, nil
}

func GetChannel(group string, model string, retry int, requestPath string, excludedChannelIDs ...map[int]struct{}) (*Channel, error) {
	var excluded map[int]struct{}
	if len(excludedChannelIDs) > 0 {
		excluded = excludedChannelIDs[0]
	}
	return GetChannelWithOptions(group, model, retry, requestPath, excluded, ChannelSelectionOptions{})
}

func GetChannelWithOptions(group string, model string, retry int, requestPath string, excludedChannelIDs map[int]struct{}, options ChannelSelectionOptions) (*Channel, error) {
	abilities, err := getHealthyAbilities(group, model, retry, requestPath, excludedChannelIDs, options)
	if err != nil {
		return nil, err
	}
	channel := Channel{}
	if len(abilities) > 0 {
		channel.Id = abilities[0].ChannelId
	} else {
		return nil, nil
	}
	err = DB.First(&channel, "id = ?", channel.Id).Error
	return &channel, err
}

type abilityRuntimeCandidate struct {
	ability   Ability
	candidate runtimeSelectionCandidate
}

func getHealthyAbilities(group string, modelName string, retry int, requestPath string, excludedChannelIDs map[int]struct{}, options ChannelSelectionOptions) ([]Ability, error) {
	var priorities []int
	err := DB.Model(&Ability{}).
		Select("DISTINCT(priority)").
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, modelName, true).
		Order("priority DESC").
		Pluck("priority", &priorities).Error
	if err != nil {
		return nil, err
	}
	if len(priorities) == 0 {
		return nil, nil
	}
	if retry >= len(priorities) {
		retry = len(priorities) - 1
	}

	retainedProbeCandidates := make([]abilityRuntimeCandidate, 0)
	for i := retry; i < len(priorities); i++ {
		var abilities []Ability
		err = DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = ?", group, modelName, true, priorities[i]).
			Order("weight DESC").
			Find(&abilities).Error
		if err != nil {
			return nil, err
		}
		abilities, err = filterAbilitiesBySelectionOptions(abilities, options)
		if err != nil {
			return nil, err
		}
		abilities = filterAbilitiesByRequestPathAndModel(abilities, requestPath, modelName)
		normalCandidates := collectAbilityRuntimeCandidates(abilities, modelName, false, excludedChannelIDs)
		probeCandidates := collectAbilityRuntimeCandidates(abilities, modelName, true, excludedChannelIDs)
		probeCandidates = filterAbilityProbeCandidates(probeCandidates, normalCandidates)
		if len(normalCandidates) > 0 {
			candidates := make([]abilityRuntimeCandidate, 0, len(retainedProbeCandidates)+len(normalCandidates)+len(probeCandidates))
			candidates = append(candidates, retainedProbeCandidates...)
			candidates = append(candidates, normalCandidates...)
			candidates = append(candidates, probeCandidates...)
			selected, ok, err := selectAbilityRuntimeCandidate(group, modelName, candidates)
			if err != nil {
				return nil, err
			}
			if ok {
				return []Ability{selected}, nil
			}
			return nil, nil
		}
		retainedProbeCandidates = append(retainedProbeCandidates, probeCandidates...)
	}
	if len(retainedProbeCandidates) > 0 {
		selected, ok, err := selectAbilityRuntimeCandidate(group, modelName, retainedProbeCandidates)
		if err != nil {
			return nil, err
		}
		if ok {
			return []Ability{selected}, nil
		}
	}
	return nil, nil
}

func collectAbilityRuntimeCandidates(abilities []Ability, modelName string, probe bool, excludedChannelIDs map[int]struct{}) []abilityRuntimeCandidate {
	candidates := make([]abilityRuntimeCandidate, 0, len(abilities))
	for _, ability := range abilities {
		if _, excluded := excludedChannelIDs[ability.ChannelId]; excluded {
			continue
		}
		var available bool
		var inflight int
		if probe {
			available, inflight = getChannelProbeRuntimeState(ability.ChannelId, modelName)
		} else {
			available, inflight = getChannelRuntimeState(ability.ChannelId, modelName)
		}
		if !available {
			continue
		}
		priority := int64(0)
		if ability.Priority != nil {
			priority = *ability.Priority
		}
		weight := int(ability.Weight)
		candidates = append(candidates, abilityRuntimeCandidate{
			ability: ability,
			candidate: runtimeSelectionCandidate{
				ChannelID:       ability.ChannelId,
				Priority:        priority,
				ModelName:       modelName,
				Weight:          weight,
				EffectiveWeight: runtimeCandidateEffectiveWeight(ability.ChannelId, modelName, weight, inflight, probe),
				Inflight:        inflight,
				Probe:           probe,
			},
		})
	}
	return candidates
}

func filterAbilityProbeCandidates(probeCandidates []abilityRuntimeCandidate, normalCandidates []abilityRuntimeCandidate) []abilityRuntimeCandidate {
	if len(probeCandidates) == 0 || len(normalCandidates) == 0 {
		return probeCandidates
	}
	normalByChannelID := make(map[int]struct{}, len(normalCandidates))
	for _, candidate := range normalCandidates {
		normalByChannelID[candidate.candidate.ChannelID] = struct{}{}
	}
	filtered := probeCandidates[:0]
	for _, candidate := range probeCandidates {
		if _, normal := normalByChannelID[candidate.candidate.ChannelID]; normal {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func selectAbilityRuntimeCandidate(group string, modelName string, candidates []abilityRuntimeCandidate) (Ability, bool, error) {
	for len(candidates) > 0 {
		runtimeCandidates := make([]runtimeSelectionCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			runtimeCandidates = append(runtimeCandidates, candidate.candidate)
		}
		selected, err := selectRuntimeCandidateWithProbeClaim(group, modelName, runtimeCandidates)
		if err != nil {
			return Ability{}, false, err
		}
		for i, candidate := range candidates {
			if candidate.candidate.ChannelID == selected.ChannelID && candidate.candidate.Probe == selected.Probe {
				return candidate.ability, true, nil
			}
			if candidate.candidate.ChannelID == selected.ChannelID {
				candidates = append(candidates[:i], candidates[i+1:]...)
				break
			}
		}
	}
	return Ability{}, false, nil
}

func filterAbilitiesBySelectionOptions(abilities []Ability, options ChannelSelectionOptions) ([]Ability, error) {
	if !options.RequireImageInputSupport || len(abilities) == 0 {
		return abilities, nil
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []*Channel
	if err := DB.Select("id", "settings").Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		return nil, err
	}
	supportsImageInputByID := make(map[int]bool, len(channels))
	for _, channel := range channels {
		supportsImageInputByID[channel.Id] = channel.SupportsImageInput()
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		if supportsImageInputByID[ability.ChannelId] {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}

// filterAbilitiesByRequestPathAndModel restricts candidates by request path and model for the DB
// (non-memory-cache) selection path. Only Advanced Custom (type 58) channels are
// path-checked: kept only when one of their routes matches requestPath and model; all other
// channel types always pass. When requestPath is empty, filtering is skipped.
func filterAbilitiesByRequestPathAndModel(abilities []Ability, requestPath string, model string) []Ability {
	if requestPath == "" || len(abilities) == 0 {
		return abilities
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []*Channel
	if err := DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		// On error, fall back to unfiltered candidates to avoid blocking selection
		return abilities
	}

	advancedConfigs := make(map[int]*dto.AdvancedCustomConfig)
	for _, channel := range channels {
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			advancedConfigs[channel.Id] = channel.GetOtherSettings().AdvancedCustom
		}
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		config, isAdvancedCustom := advancedConfigs[ability.ChannelId]
		if !isAdvancedCustom {
			filtered = append(filtered, ability)
			continue
		}
		if config != nil && config.SupportsPathForModel(requestPath, model) {
			filtered = append(filtered, ability)
		}
	}
	return filtered
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// First delete all abilities of this channel
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	return DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	return DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	return successCount, failCount, nil
}
