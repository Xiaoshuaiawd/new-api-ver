package service

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

const (
	alipayF2FDefaultGateway = "https://openapi.alipay.com/gateway.do"
	alipayF2FDefaultCharset = "utf-8"
	alipayF2FDefaultVersion = "1.0"
	alipayF2FSignTypeRSA2   = "RSA2"
)

type AlipayF2FConfig struct {
	AppID      string
	SellerID   string
	Gateway    string
	PrivateKey string
	PublicKey  string
	HTTPClient *http.Client
}

type AlipayF2FClient struct {
	appID      string
	sellerID   string
	gateway    string
	httpClient *http.Client
	privateKey *rsa.PrivateKey
	alipayKey  *rsa.PublicKey
	charset    string
	version    string
	signType   string
}

type AlipayF2FPrecreateRequest struct {
	OutTradeNo     string
	TotalAmount    string
	Subject        string
	NotifyURL      string
	TimeoutExpress string
}

type AlipayF2FPrecreateResponse struct {
	Code       string `json:"code"`
	Msg        string `json:"msg"`
	SubCode    string `json:"sub_code"`
	SubMsg     string `json:"sub_msg"`
	OutTradeNo string `json:"out_trade_no"`
	QRCode     string `json:"qr_code"`
}

type AlipayF2FTradeQueryResponse struct {
	Code           string `json:"code"`
	Msg            string `json:"msg"`
	SubCode        string `json:"sub_code"`
	SubMsg         string `json:"sub_msg"`
	OutTradeNo     string `json:"out_trade_no"`
	TradeNo        string `json:"trade_no"`
	TradeStatus    string `json:"trade_status"`
	TotalAmount    string `json:"total_amount"`
	BuyerPayAmount string `json:"buyer_pay_amount"`
}

type AlipayF2FTradeCloseResponse struct {
	Code       string `json:"code"`
	Msg        string `json:"msg"`
	SubCode    string `json:"sub_code"`
	SubMsg     string `json:"sub_msg"`
	OutTradeNo string `json:"out_trade_no"`
	TradeNo    string `json:"trade_no"`
}

type AlipayF2FNotification struct {
	NotifyTime     string
	NotifyType     string
	NotifyID       string
	AppID          string
	SellerID       string
	OutTradeNo     string
	TradeNo        string
	TradeStatus    string
	TotalAmount    string
	BuyerPayAmount string
}

func NewAlipayF2FClient(cfg AlipayF2FConfig) (*AlipayF2FClient, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, errors.New("missing alipay app id")
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return nil, errors.New("missing alipay merchant private key")
	}
	if strings.TrimSpace(cfg.PublicKey) == "" {
		return nil, errors.New("missing alipay public key")
	}

	privateKey, err := parseAlipayF2FPrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	publicKey, err := parseAlipayF2FPublicKey(cfg.PublicKey)
	if err != nil {
		return nil, err
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	return &AlipayF2FClient{
		appID:      strings.TrimSpace(cfg.AppID),
		sellerID:   strings.TrimSpace(cfg.SellerID),
		gateway:    normalizeAlipayF2FGateway(cfg.Gateway),
		httpClient: httpClient,
		privateKey: privateKey,
		alipayKey:  publicKey,
		charset:    alipayF2FDefaultCharset,
		version:    alipayF2FDefaultVersion,
		signType:   alipayF2FSignTypeRSA2,
	}, nil
}

func NewConfiguredAlipayF2FClient() (*AlipayF2FClient, error) {
	return NewAlipayF2FClient(AlipayF2FConfig{
		AppID:      setting.AlipayF2FAppID,
		SellerID:   setting.AlipayF2FSellerID,
		Gateway:    setting.AlipayF2FGateway,
		PrivateKey: setting.AlipayF2FPrivateKey,
		PublicKey:  setting.AlipayF2FPublicKey,
	})
}

func normalizeAlipayF2FGateway(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return alipayF2FDefaultGateway
	}
	return trimmed
}

func (c *AlipayF2FClient) buildRequestValues(method string, bizContent any) (url.Values, error) {
	bizContentBytes, err := common.Marshal(bizContent)
	if err != nil {
		return nil, err
	}

	values := url.Values{}
	values.Set("app_id", c.appID)
	values.Set("method", method)
	values.Set("format", "JSON")
	values.Set("charset", c.charset)
	values.Set("sign_type", c.signType)
	values.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	values.Set("version", c.version)
	values.Set("biz_content", string(bizContentBytes))

	if err := c.applySignature(values); err != nil {
		return nil, err
	}

	return values, nil
}

func buildAlipayF2FSignContent(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "sign" || key == "sign_type" {
			continue
		}
		if strings.TrimSpace(values.Get(key)) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	return strings.Join(parts, "&")
}

func (c *AlipayF2FClient) applySignature(values url.Values) error {
	values.Del("sign")
	signature, err := c.signContent(buildAlipayF2FSignContent(values))
	if err != nil {
		return err
	}
	values.Set("sign", signature)
	return nil
}

func (c *AlipayF2FClient) signContent(content string) (string, error) {
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func (c *AlipayF2FClient) verifyContent(content string, signature string) error {
	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(c.alipayKey, crypto.SHA256, digest[:], signatureBytes)
}

func (c *AlipayF2FClient) doGatewayRequest(ctx context.Context, values url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gateway, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset="+c.charset)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("alipay gateway request failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *AlipayF2FClient) parseResponse(body []byte, method string) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := common.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}

	responseKey := strings.ReplaceAll(method, ".", "_") + "_response"
	rawResponse, ok := envelope[responseKey]
	if !ok || len(bytes.TrimSpace(rawResponse)) == 0 {
		if rawError, hasError := envelope["error_response"]; hasError {
			return nil, fmt.Errorf("alipay gateway returned error: %s", string(rawError))
		}
		return nil, fmt.Errorf("alipay gateway response missing %s", responseKey)
	}

	var signature string
	if rawSignature, ok := envelope["sign"]; ok {
		if err := common.Unmarshal(rawSignature, &signature); err != nil {
			return nil, err
		}
	}
	if signature == "" {
		return nil, errors.New("alipay gateway response missing sign")
	}

	if err := c.verifyContent(string(rawResponse), signature); err != nil {
		return nil, err
	}

	return rawResponse, nil
}

func (c *AlipayF2FClient) Precreate(ctx context.Context, req *AlipayF2FPrecreateRequest) (*AlipayF2FPrecreateResponse, error) {
	bizContent := map[string]any{
		"out_trade_no":    req.OutTradeNo,
		"total_amount":    req.TotalAmount,
		"subject":         req.Subject,
		"timeout_express": req.TimeoutExpress,
		"product_code":    "FACE_TO_FACE_PAYMENT",
	}
	if c.sellerID != "" {
		bizContent["seller_id"] = c.sellerID
	}

	values, err := c.buildRequestValues("alipay.trade.precreate", bizContent)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(req.NotifyURL) != "" {
		values.Set("notify_url", strings.TrimSpace(req.NotifyURL))
		if err := c.applySignature(values); err != nil {
			return nil, err
		}
	}

	body, err := c.doGatewayRequest(ctx, values)
	if err != nil {
		return nil, err
	}

	rawResponse, err := c.parseResponse(body, "alipay.trade.precreate")
	if err != nil {
		return nil, err
	}

	resp := &AlipayF2FPrecreateResponse{}
	if err := common.Unmarshal(rawResponse, resp); err != nil {
		return nil, err
	}
	if resp.Code != "10000" {
		return nil, formatAlipayF2FAPIError(resp.Code, resp.Msg, resp.SubCode, resp.SubMsg)
	}
	return resp, nil
}

func (c *AlipayF2FClient) Query(ctx context.Context, outTradeNo string) (*AlipayF2FTradeQueryResponse, error) {
	bizContent := map[string]any{
		"out_trade_no": outTradeNo,
	}
	if c.sellerID != "" {
		bizContent["seller_id"] = c.sellerID
	}

	values, err := c.buildRequestValues("alipay.trade.query", bizContent)
	if err != nil {
		return nil, err
	}

	body, err := c.doGatewayRequest(ctx, values)
	if err != nil {
		return nil, err
	}

	rawResponse, err := c.parseResponse(body, "alipay.trade.query")
	if err != nil {
		return nil, err
	}

	resp := &AlipayF2FTradeQueryResponse{}
	if err := common.Unmarshal(rawResponse, resp); err != nil {
		return nil, err
	}
	if resp.Code != "10000" {
		return nil, formatAlipayF2FAPIError(resp.Code, resp.Msg, resp.SubCode, resp.SubMsg)
	}
	return resp, nil
}

func (c *AlipayF2FClient) Close(ctx context.Context, outTradeNo string) (*AlipayF2FTradeCloseResponse, error) {
	bizContent := map[string]any{
		"out_trade_no": outTradeNo,
	}
	if c.sellerID != "" {
		bizContent["seller_id"] = c.sellerID
	}

	values, err := c.buildRequestValues("alipay.trade.close", bizContent)
	if err != nil {
		return nil, err
	}

	body, err := c.doGatewayRequest(ctx, values)
	if err != nil {
		return nil, err
	}

	rawResponse, err := c.parseResponse(body, "alipay.trade.close")
	if err != nil {
		return nil, err
	}

	resp := &AlipayF2FTradeCloseResponse{}
	if err := common.Unmarshal(rawResponse, resp); err != nil {
		return nil, err
	}
	if resp.Code != "10000" {
		return nil, formatAlipayF2FAPIError(resp.Code, resp.Msg, resp.SubCode, resp.SubMsg)
	}
	return resp, nil
}

func (c *AlipayF2FClient) VerifyNotification(params map[string]string) (*AlipayF2FNotification, error) {
	signature := strings.TrimSpace(params["sign"])
	if signature == "" {
		return nil, errors.New("alipay notification missing sign")
	}
	if signType := strings.TrimSpace(params["sign_type"]); signType != "" && !strings.EqualFold(signType, c.signType) {
		return nil, fmt.Errorf("unsupported alipay sign type: %s", signType)
	}

	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	if err := c.verifyContent(buildAlipayF2FSignContent(values), signature); err != nil {
		return nil, err
	}

	notification := &AlipayF2FNotification{
		NotifyTime:     params["notify_time"],
		NotifyType:     params["notify_type"],
		NotifyID:       params["notify_id"],
		AppID:          params["app_id"],
		SellerID:       params["seller_id"],
		OutTradeNo:     params["out_trade_no"],
		TradeNo:        params["trade_no"],
		TradeStatus:    params["trade_status"],
		TotalAmount:    params["total_amount"],
		BuyerPayAmount: params["buyer_pay_amount"],
	}

	if c.appID != "" && notification.AppID != "" && notification.AppID != c.appID {
		return nil, fmt.Errorf("unexpected alipay app id: %s", notification.AppID)
	}
	if c.sellerID != "" && notification.SellerID != "" && notification.SellerID != c.sellerID {
		return nil, fmt.Errorf("unexpected alipay seller id: %s", notification.SellerID)
	}

	return notification, nil
}

func formatAlipayF2FAPIError(code string, msg string, subCode string, subMsg string) error {
	if subCode != "" || subMsg != "" {
		return fmt.Errorf("alipay api error code=%s msg=%s sub_code=%s sub_msg=%s", code, msg, subCode, subMsg)
	}
	return fmt.Errorf("alipay api error code=%s msg=%s", code, msg)
}

func parseAlipayF2FPrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, err := decodeAlipayF2FPEMBlock(raw, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}

	if privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return privateKey, nil
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	privateKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("alipay merchant private key is not RSA")
	}
	return privateKey, nil
}

func parseAlipayF2FPublicKey(raw string) (*rsa.PublicKey, error) {
	block, err := decodeAlipayF2FPEMBlock(raw, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}

	if publicKey, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("alipay public key is not RSA")
		}
		return rsaPublicKey, nil
	}

	return x509.ParsePKCS1PublicKey(block.Bytes)
}

func decodeAlipayF2FPEMBlock(raw string, defaultType string) (*pem.Block, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty pem content")
	}

	if strings.Contains(trimmed, "BEGIN") {
		block, _ := pem.Decode([]byte(trimmed))
		if block == nil {
			return nil, errors.New("invalid pem content")
		}
		return block, nil
	}

	base64Content := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		default:
			return r
		}
	}, trimmed)
	decoded, err := base64.StdEncoding.DecodeString(base64Content)
	if err != nil {
		return nil, err
	}

	block := &pem.Block{
		Type:  defaultType,
		Bytes: decoded,
	}
	return block, nil
}
