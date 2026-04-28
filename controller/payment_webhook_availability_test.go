package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestStripeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	setting.StripeWebhookSecret = ""
	setting.StripeApiSecret = "sk_test_123"
	setting.StripePriceId = "price_123"
	require.False(t, isStripeWebhookEnabled())

	setting.StripeWebhookSecret = "whsec_test"
	require.True(t, isStripeWebhookEnabled())

	setting.StripePriceId = ""
	require.False(t, isStripeWebhookEnabled())
}

func TestCreemWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	originalAPIKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalWebhookSecret := setting.CreemWebhookSecret
	t.Cleanup(func() {
		setting.CreemApiKey = originalAPIKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalWebhookSecret
	})

	setting.CreemWebhookSecret = ""
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = `[{"productId":"prod_123"}]`
	require.False(t, isCreemWebhookEnabled())

	setting.CreemWebhookSecret = "creem_secret"
	require.True(t, isCreemWebhookEnabled())

	setting.CreemProducts = "[]"
	require.False(t, isCreemWebhookEnabled())
}

func TestWaffoWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	originalEnabled := setting.WaffoEnabled
	originalSandbox := setting.WaffoSandbox
	originalAPIKey := setting.WaffoApiKey
	originalPrivateKey := setting.WaffoPrivateKey
	originalPublicCert := setting.WaffoPublicCert
	originalSandboxAPIKey := setting.WaffoSandboxApiKey
	originalSandboxPrivateKey := setting.WaffoSandboxPrivateKey
	originalSandboxPublicCert := setting.WaffoSandboxPublicCert
	t.Cleanup(func() {
		setting.WaffoEnabled = originalEnabled
		setting.WaffoSandbox = originalSandbox
		setting.WaffoApiKey = originalAPIKey
		setting.WaffoPrivateKey = originalPrivateKey
		setting.WaffoPublicCert = originalPublicCert
		setting.WaffoSandboxApiKey = originalSandboxAPIKey
		setting.WaffoSandboxPrivateKey = originalSandboxPrivateKey
		setting.WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = ""
	setting.WaffoPrivateKey = "private"
	setting.WaffoPublicCert = "public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoApiKey = "api"
	require.True(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = false
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = true
	setting.WaffoSandbox = true
	setting.WaffoSandboxApiKey = ""
	setting.WaffoSandboxPrivateKey = "sandbox_private"
	setting.WaffoSandboxPublicCert = "sandbox_public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoSandboxApiKey = "sandbox_api"
	require.True(t, isWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	originalEnabled := setting.WaffoPancakeEnabled
	originalSandbox := setting.WaffoPancakeSandbox
	originalMerchantID := setting.WaffoPancakeMerchantID
	originalPrivateKey := setting.WaffoPancakePrivateKey
	originalWebhookPublicKey := setting.WaffoPancakeWebhookPublicKey
	originalWebhookTestKey := setting.WaffoPancakeWebhookTestKey
	originalStoreID := setting.WaffoPancakeStoreID
	originalProductID := setting.WaffoPancakeProductID
	t.Cleanup(func() {
		setting.WaffoPancakeEnabled = originalEnabled
		setting.WaffoPancakeSandbox = originalSandbox
		setting.WaffoPancakeMerchantID = originalMerchantID
		setting.WaffoPancakePrivateKey = originalPrivateKey
		setting.WaffoPancakeWebhookPublicKey = originalWebhookPublicKey
		setting.WaffoPancakeWebhookTestKey = originalWebhookTestKey
		setting.WaffoPancakeStoreID = originalStoreID
		setting.WaffoPancakeProductID = originalProductID
	})

	setting.WaffoPancakeEnabled = true
	setting.WaffoPancakeSandbox = false
	setting.WaffoPancakeMerchantID = "merchant"
	setting.WaffoPancakePrivateKey = "private"
	setting.WaffoPancakeStoreID = "store"
	setting.WaffoPancakeProductID = "product"
	setting.WaffoPancakeWebhookPublicKey = ""
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeWebhookPublicKey = "public"
	require.True(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeEnabled = false
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeEnabled = true
	setting.WaffoPancakeSandbox = true
	setting.WaffoPancakeWebhookTestKey = ""
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeWebhookTestKey = "test_public"
	require.True(t, isWaffoPancakeWebhookEnabled())
}

func TestEpayWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = ""
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	require.False(t, isEpayWebhookEnabled())

	operation_setting.EpayKey = "epay_key"
	require.True(t, isEpayWebhookEnabled())

	operation_setting.PayMethods = nil
	require.False(t, isEpayWebhookEnabled())
}

func TestAlipayF2FWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	originalEnabled := setting.AlipayF2FEnabled
	originalAppID := setting.AlipayF2FAppID
	originalSellerID := setting.AlipayF2FSellerID
	originalPrivateKey := setting.AlipayF2FPrivateKey
	originalPublicKey := setting.AlipayF2FPublicKey
	originalMinTopUp := setting.AlipayF2FMinTopUp
	t.Cleanup(func() {
		setting.AlipayF2FEnabled = originalEnabled
		setting.AlipayF2FAppID = originalAppID
		setting.AlipayF2FSellerID = originalSellerID
		setting.AlipayF2FPrivateKey = originalPrivateKey
		setting.AlipayF2FPublicKey = originalPublicKey
		setting.AlipayF2FMinTopUp = originalMinTopUp
	})

	setting.AlipayF2FEnabled = true
	setting.AlipayF2FAppID = "2026000000000000"
	setting.AlipayF2FSellerID = ""
	setting.AlipayF2FPrivateKey = "merchant-private-key"
	setting.AlipayF2FPublicKey = ""
	setting.AlipayF2FMinTopUp = 1
	require.False(t, isAlipayF2FWebhookEnabled())

	setting.AlipayF2FPublicKey = "alipay-public-key"
	require.True(t, isAlipayF2FWebhookEnabled())

	setting.AlipayF2FEnabled = false
	require.False(t, isAlipayF2FWebhookEnabled())

	setting.AlipayF2FEnabled = true
	setting.AlipayF2FMinTopUp = 0
	require.False(t, isAlipayF2FWebhookEnabled())
}
