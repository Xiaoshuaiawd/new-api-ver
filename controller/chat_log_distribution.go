package controller

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetDataDistribution(c *gin.Context) {
	startTime, endTime, err := parseDataDistributionRange(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	status := strings.TrimSpace(c.Query("status"))
	result, err := model.GetChatLogDistribution(startTime, endTime, status)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func parseDataDistributionRange(c *gin.Context) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	end := now

	if val := strings.TrimSpace(c.Query("start_timestamp")); val != "" {
		parsed, parseErr := parseFlexibleUnixTime(val, loc)
		if parseErr != nil {
			return time.Time{}, time.Time{}, errors.New("invalid start_timestamp")
		}
		start = parsed
	} else if val := strings.TrimSpace(c.Query("start_time")); val != "" {
		parsed, parseErr := parseFlexibleUnixTime(val, loc)
		if parseErr != nil {
			return time.Time{}, time.Time{}, errors.New("invalid start_time")
		}
		start = parsed
	}

	if val := strings.TrimSpace(c.Query("end_timestamp")); val != "" {
		parsed, parseErr := parseFlexibleUnixTime(val, loc)
		if parseErr != nil {
			return time.Time{}, time.Time{}, errors.New("invalid end_timestamp")
		}
		end = parsed
	} else if val := strings.TrimSpace(c.Query("end_time")); val != "" {
		parsed, parseErr := parseFlexibleUnixTime(val, loc)
		if parseErr != nil {
			return time.Time{}, time.Time{}, errors.New("invalid end_time")
		}
		end = parsed
	}

	if end.Before(start) {
		start, end = end, start
	}
	return start, end, nil
}

func parseFlexibleUnixTime(value string, loc *time.Location) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("empty time")
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		if unix > 1_000_000_000_000 {
			return time.UnixMilli(unix).In(loc), nil
		}
		return time.Unix(unix, 0).In(loc), nil
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t.In(loc), nil
		}
	}
	return time.Time{}, errors.New("unsupported time format")
}
