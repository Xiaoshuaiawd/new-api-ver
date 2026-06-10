package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type redemptionControllerResponse struct {
	Success bool                         `json:"success"`
	Message string                       `json:"message"`
	Data    model.RedemptionRedeemResult `json:"data"`
}

func setupRedemptionControllerTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Redemption{},
		&model.Log{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
	))
}

func TestTopUpReturnsRedemptionResultObject(t *testing.T) {
	setupRedemptionControllerTestDB(t)

	confirmPaymentComplianceForTest(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       8801,
		Username: "redeem_controller_user",
		Status:   common.UserStatusEnabled,
		Quota:    0,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Redemption{
		Name:        "quota controller",
		Key:         "quota-controller-key",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       123,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 8801)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/topup", bytes.NewBufferString(`{"key":"quota-controller-key"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	TopUp(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response redemptionControllerResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, model.RedemptionRewardTypeQuota, response.Data.Type)
	assert.Equal(t, 123, response.Data.Quota)
	assert.Zero(t, response.Data.PlanId)
	assert.Empty(t, response.Data.PlanTitle)
}
