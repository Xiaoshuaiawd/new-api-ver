package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func generateRSAPrivateKeyPEMForAlipayF2FTest(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der := x509.MarshalPKCS1PrivateKey(privateKey)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: der,
	}
	return string(pem.EncodeToMemory(block)), privateKey
}

func generateRSAPublicKeyPEMForAlipayF2FTest(t *testing.T, privateKey *rsa.PrivateKey) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)

	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}
	return string(pem.EncodeToMemory(block))
}

func signAlipayF2FContentForTest(t *testing.T, privateKey *rsa.PrivateKey, content string) string {
	t.Helper()

	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(signature)
}

func verifyAlipayF2FSignatureForTest(t *testing.T, publicKey *rsa.PublicKey, content string, signature string) {
	t.Helper()

	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	require.NoError(t, err)

	digest := sha256.Sum256([]byte(content))
	require.NoError(t, rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signatureBytes))
}

func TestAlipayF2FBuildSignedValues(t *testing.T) {
	merchantPrivateKeyPEM, merchantPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)
	_, alipayPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)

	client, err := NewAlipayF2FClient(AlipayF2FConfig{
		AppID:      "2026000000000000",
		SellerID:   "2088000000000000",
		Gateway:    "https://example.com/gateway.do",
		PrivateKey: merchantPrivateKeyPEM,
		PublicKey:  generateRSAPublicKeyPEMForAlipayF2FTest(t, alipayPrivateKey),
	})
	require.NoError(t, err)

	values, err := client.buildRequestValues("alipay.trade.precreate", map[string]any{
		"out_trade_no":    "trade-123",
		"total_amount":    "12.34",
		"subject":         "new-api Recharge 12",
		"timeout_express": "30m",
	})
	require.NoError(t, err)

	require.Equal(t, "2026000000000000", values.Get("app_id"))
	require.Equal(t, "alipay.trade.precreate", values.Get("method"))
	require.Equal(t, "RSA2", values.Get("sign_type"))
	require.NotEmpty(t, values.Get("biz_content"))
	require.NotEmpty(t, values.Get("sign"))

	content := buildAlipayF2FSignContent(values)
	verifyAlipayF2FSignatureForTest(t, &merchantPrivateKey.PublicKey, content, values.Get("sign"))
}

func TestAlipayF2FVerifyNotification(t *testing.T) {
	merchantPrivateKeyPEM, _ := generateRSAPrivateKeyPEMForAlipayF2FTest(t)
	_, alipayPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)

	client, err := NewAlipayF2FClient(AlipayF2FConfig{
		AppID:      "2026000000000000",
		SellerID:   "2088000000000000",
		Gateway:    "https://example.com/gateway.do",
		PrivateKey: merchantPrivateKeyPEM,
		PublicKey:  generateRSAPublicKeyPEMForAlipayF2FTest(t, alipayPrivateKey),
	})
	require.NoError(t, err)

	params := map[string]string{
		"notify_time":      "2026-04-28 12:00:00",
		"notify_type":      "trade_status_sync",
		"notify_id":        "notify-id",
		"app_id":           "2026000000000000",
		"seller_id":        "2088000000000000",
		"out_trade_no":     "trade-123",
		"trade_status":     "TRADE_SUCCESS",
		"total_amount":     "12.34",
		"sign_type":        "RSA2",
		"buyer_pay_amount": "12.34",
	}
	params["sign"] = signAlipayF2FContentForTest(t, alipayPrivateKey, buildAlipayF2FSignContentFromMap(params))

	verified, err := client.VerifyNotification(params)
	require.NoError(t, err)
	require.Equal(t, "trade-123", verified.OutTradeNo)
	require.Equal(t, "TRADE_SUCCESS", verified.TradeStatus)

	params["total_amount"] = "99.99"
	_, err = client.VerifyNotification(params)
	require.Error(t, err)

}

func TestAlipayF2FPrecreateParsesSignedResponse(t *testing.T) {
	merchantPrivateKeyPEM, merchantPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)
	_, alipayPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "alipay.trade.precreate", r.Form.Get("method"))
		require.NotEmpty(t, r.Form.Get("biz_content"))

		signature := r.Form.Get("sign")
		require.NotEmpty(t, signature)
		copied := url.Values{}
		for key, values := range r.Form {
			for _, value := range values {
				copied.Add(key, value)
			}
		}
		copied.Del("sign")
		verifyAlipayF2FSignatureForTest(t, &merchantPrivateKey.PublicKey, buildAlipayF2FSignContent(copied), signature)

		responseNode := map[string]any{
			"code":         "10000",
			"msg":          "Success",
			"out_trade_no": "trade-123",
			"qr_code":      "https://qr.example.com/pay/trade-123",
		}
		responseNodeBytes, err := common.Marshal(responseNode)
		require.NoError(t, err)
		response := map[string]any{
			"alipay_trade_precreate_response": responseNode,
			"sign": signAlipayF2FContentForTest(
				t,
				alipayPrivateKey,
				string(responseNodeBytes),
			),
		}
		body, err := common.Marshal(response)
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client, err := NewAlipayF2FClient(AlipayF2FConfig{
		AppID:      "2026000000000000",
		SellerID:   "2088000000000000",
		Gateway:    server.URL,
		PrivateKey: merchantPrivateKeyPEM,
		PublicKey:  generateRSAPublicKeyPEMForAlipayF2FTest(t, alipayPrivateKey),
	})
	require.NoError(t, err)

	resp, err := client.Precreate(context.Background(), &AlipayF2FPrecreateRequest{
		OutTradeNo:     "trade-123",
		TotalAmount:    "12.34",
		Subject:        "new-api Recharge 12",
		TimeoutExpress: "30m",
		NotifyURL:      "https://example.com/notify",
	})
	require.NoError(t, err)
	require.Equal(t, "trade-123", resp.OutTradeNo)
	require.Equal(t, "https://qr.example.com/pay/trade-123", resp.QRCode)

}

func TestAlipayF2FQueryAndCloseUseExpectedMethods(t *testing.T) {
	merchantPrivateKeyPEM, _ := generateRSAPrivateKeyPEMForAlipayF2FTest(t)
	_, alipayPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())

		var responseNode map[string]any
		switch r.Form.Get("method") {
		case "alipay.trade.query":
			require.Contains(t, r.Form.Get("biz_content"), `"out_trade_no":"trade-query"`)
			responseNode = map[string]any{
				"code":         "10000",
				"msg":          "Success",
				"out_trade_no": "trade-query",
				"trade_status": "WAIT_BUYER_PAY",
			}
		case "alipay.trade.close":
			require.Contains(t, r.Form.Get("biz_content"), `"out_trade_no":"trade-close"`)
			responseNode = map[string]any{
				"code":         "10000",
				"msg":          "Success",
				"out_trade_no": "trade-close",
			}
		default:
			t.Fatalf("unexpected method %q", r.Form.Get("method"))
		}

		responseKey := strings.ReplaceAll(r.Form.Get("method"), ".", "_") + "_response"
		responseNodeBytes, err := common.Marshal(responseNode)
		require.NoError(t, err)
		response := map[string]any{
			responseKey: responseNode,
			"sign": signAlipayF2FContentForTest(
				t,
				alipayPrivateKey,
				string(responseNodeBytes),
			),
		}
		body, err := common.Marshal(response)
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client, err := NewAlipayF2FClient(AlipayF2FConfig{
		AppID:      "2026000000000000",
		SellerID:   "2088000000000000",
		Gateway:    server.URL,
		PrivateKey: merchantPrivateKeyPEM,
		PublicKey:  generateRSAPublicKeyPEMForAlipayF2FTest(t, alipayPrivateKey),
	})
	require.NoError(t, err)

	queryResp, err := client.Query(context.Background(), "trade-query")
	require.NoError(t, err)
	require.Equal(t, "WAIT_BUYER_PAY", queryResp.TradeStatus)

	closeResp, err := client.Close(context.Background(), "trade-close")
	require.NoError(t, err)
	require.Equal(t, "trade-close", closeResp.OutTradeNo)
}

func buildAlipayF2FSignContentFromMap(params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return buildAlipayF2FSignContent(values)
}

func TestBuildAlipayF2FSignContentSkipsEmptyAndSignatureFields(t *testing.T) {
	values := url.Values{}
	values.Set("b", "2")
	values.Set("a", "1")
	values.Set("sign", "should-skip")
	values.Set("empty", "")
	values.Set("sign_type", "RSA2")

	require.Equal(t, "a=1&b=2", buildAlipayF2FSignContent(values))
}

func TestNormalizeAlipayF2FGatewayUsesProductionDefault(t *testing.T) {
	require.Equal(t, "https://openapi.alipay.com/gateway.do", normalizeAlipayF2FGateway(""))
	require.Equal(t, "https://openapi.alipay.com/gateway.do", normalizeAlipayF2FGateway("   "))
	require.Equal(t, "https://sandbox.example.com", normalizeAlipayF2FGateway("https://sandbox.example.com"))
}

func TestAlipayF2FParseResponseRejectsMissingSign(t *testing.T) {
	merchantPrivateKeyPEM, _ := generateRSAPrivateKeyPEMForAlipayF2FTest(t)
	_, alipayPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)

	client, err := NewAlipayF2FClient(AlipayF2FConfig{
		AppID:      "2026000000000000",
		SellerID:   "2088000000000000",
		Gateway:    "https://example.com/gateway.do",
		PrivateKey: merchantPrivateKeyPEM,
		PublicKey:  generateRSAPublicKeyPEMForAlipayF2FTest(t, alipayPrivateKey),
	})
	require.NoError(t, err)

	responseNode := map[string]any{
		"code":         "10000",
		"out_trade_no": "trade-123",
	}
	body, err := common.Marshal(map[string]any{
		"alipay_trade_query_response": responseNode,
	})
	require.NoError(t, err)

	_, err = client.parseResponse(body, "alipay.trade.query")
	require.Error(t, err)
	require.Contains(t, err.Error(), "sign")
}

func TestAlipayF2FResponseVerificationDetectsTampering(t *testing.T) {
	merchantPrivateKeyPEM, _ := generateRSAPrivateKeyPEMForAlipayF2FTest(t)
	_, alipayPrivateKey := generateRSAPrivateKeyPEMForAlipayF2FTest(t)

	client, err := NewAlipayF2FClient(AlipayF2FConfig{
		AppID:      "2026000000000000",
		SellerID:   "2088000000000000",
		Gateway:    "https://example.com/gateway.do",
		PrivateKey: merchantPrivateKeyPEM,
		PublicKey:  generateRSAPublicKeyPEMForAlipayF2FTest(t, alipayPrivateKey),
	})
	require.NoError(t, err)

	responseNode := map[string]any{
		"code":         "10000",
		"out_trade_no": "trade-123",
	}
	responseNodeBytes, err := common.Marshal(responseNode)
	require.NoError(t, err)
	body, err := common.Marshal(map[string]any{
		"alipay_trade_query_response": map[string]any{
			"code":         "10000",
			"out_trade_no": "trade-123",
		},
		"sign": signAlipayF2FContentForTest(t, alipayPrivateKey, fmt.Sprintf("%s-tampered", string(responseNodeBytes))),
	})
	require.NoError(t, err)

	_, err = client.parseResponse(body, "alipay.trade.query")
	require.Error(t, err)
}
