package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	referralRewardSettlementTickInterval = 1 * time.Minute
	referralRewardSettlementBatchSize    = 200
)

var (
	referralRewardSettlementOnce    sync.Once
	referralRewardSettlementRunning atomic.Bool
)

func StartReferralRewardSettlementTask() {
	referralRewardSettlementOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("referral reward settlement task started: tick=%s", referralRewardSettlementTickInterval))
			ticker := time.NewTicker(referralRewardSettlementTickInterval)
			defer ticker.Stop()

			runReferralRewardSettlementOnce()
			for range ticker.C {
				runReferralRewardSettlementOnce()
			}
		})
	})
}

func runReferralRewardSettlementOnce() {
	if !referralRewardSettlementRunning.CompareAndSwap(false, true) {
		return
	}
	defer referralRewardSettlementRunning.Store(false)

	for {
		settled, err := model.SettleDueReferralRewards(referralRewardSettlementBatchSize)
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("referral reward settlement failed: %v", err))
			return
		}
		if settled == 0 || settled < referralRewardSettlementBatchSize {
			return
		}
	}
}
