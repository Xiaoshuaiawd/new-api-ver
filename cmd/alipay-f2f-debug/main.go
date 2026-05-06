package main

import (
	"context"
	"html"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/service"
)

type debugPageData struct {
	AppID          string
	SellerID       string
	Gateway        string
	PrivateKey     string
	PublicKey      string
	NotifyURL      string
	Amount         string
	Subject        string
	TimeoutExpress string
	OutTradeNo     string
	Success        bool
	ErrorMessage   string
	QRCode         string
	SignContent    string
	RawRequestBody string
	RawResponseBody string
	RequestValues  map[string]string
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleDebugPage)

	addr := os.Getenv("ALIPAY_F2F_DEBUG_ADDR")
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:3006"
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	println("Alipay F2F debug page:", "http://"+addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}

func handleDebugPage(w http.ResponseWriter, r *http.Request) {
	data := debugPageData{
		Gateway:        "https://openapi.alipay.com/gateway.do",
		Amount:         "1.00",
		Subject:        "new-api Alipay F2F Debug",
		TimeoutExpress: "30m",
		OutTradeNo:     "DBG" + strconv.FormatInt(time.Now().UnixMilli(), 10),
	}

	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		data.AppID = r.FormValue("app_id")
		data.SellerID = r.FormValue("seller_id")
		data.Gateway = fallback(r.FormValue("gateway"), data.Gateway)
		data.PrivateKey = r.FormValue("private_key")
		data.PublicKey = r.FormValue("public_key")
		data.NotifyURL = r.FormValue("notify_url")
		data.Amount = fallback(r.FormValue("amount"), data.Amount)
		data.Subject = fallback(r.FormValue("subject"), data.Subject)
		data.TimeoutExpress = fallback(r.FormValue("timeout_express"), data.TimeoutExpress)
		data.OutTradeNo = fallback(r.FormValue("out_trade_no"), data.OutTradeNo)

		client, err := service.NewAlipayF2FClient(service.AlipayF2FConfig{
			AppID:      data.AppID,
			SellerID:   data.SellerID,
			Gateway:    data.Gateway,
			PrivateKey: data.PrivateKey,
			PublicKey:  data.PublicKey,
		})
		if err != nil {
			data.ErrorMessage = err.Error()
		} else {
			result, err := client.DebugPrecreate(context.Background(), &service.AlipayF2FPrecreateRequest{
				OutTradeNo:     data.OutTradeNo,
				TotalAmount:    data.Amount,
				Subject:        data.Subject,
				NotifyURL:      data.NotifyURL,
				TimeoutExpress: data.TimeoutExpress,
			})
			if result != nil {
				data.SignContent = result.SignContent
				data.RawRequestBody = result.RawRequestBody
				data.RawResponseBody = result.RawResponseBody
				data.RequestValues = result.RequestValues
				if result.Response != nil {
					data.QRCode = result.Response.QRCode
				}
			}
			if err != nil {
				data.ErrorMessage = err.Error()
			} else {
				data.Success = true
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderDebugPage(data)))
}

func fallback(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func renderDebugPage(data debugPageData) string {
	var requestValuesBuilder strings.Builder
	for _, key := range []string{"app_id", "method", "format", "charset", "sign_type", "timestamp", "version", "notify_url", "biz_content", "sign"} {
		if value, ok := data.RequestValues[key]; ok {
			requestValuesBuilder.WriteString("<tr><td>")
			requestValuesBuilder.WriteString(html.EscapeString(key))
			requestValuesBuilder.WriteString("</td><td><pre>")
			requestValuesBuilder.WriteString(html.EscapeString(value))
			requestValuesBuilder.WriteString("</pre></td></tr>")
		}
	}

	statusClass := "status-idle"
	statusText := "等待测试"
	if data.Success {
		statusClass = "status-ok"
		statusText = "测试成功，已拿到二维码"
	} else if strings.TrimSpace(data.ErrorMessage) != "" {
		statusClass = "status-error"
		statusText = "测试失败"
	}

	qrBlock := ""
	if strings.TrimSpace(data.QRCode) != "" {
		qrBlock = `<div class="panel"><h3>二维码链接</h3><pre>` + html.EscapeString(data.QRCode) + `</pre></div>`
	}

	return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>支付宝当面付联调页</title>
  <style>
    :root {
      --bg: #f4efe7;
      --panel: #fffdf8;
      --ink: #1b1f23;
      --muted: #6b7280;
      --line: #e7dccb;
      --brand: #0f6fff;
      --brand-soft: #dce9ff;
      --ok: #0e9f6e;
      --ok-soft: #dff7ec;
      --bad: #d14343;
      --bad-soft: #fde7e7;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "SF Pro SC","PingFang SC","Helvetica Neue",sans-serif;
      color: var(--ink);
      background:
        radial-gradient(circle at top left, #fff4d8 0, transparent 28%),
        radial-gradient(circle at right 20%, #dbeafe 0, transparent 25%),
        linear-gradient(180deg, #f8f3eb 0%, var(--bg) 100%);
    }
    .shell {
      max-width: 1180px;
      margin: 0 auto;
      padding: 32px 20px 56px;
    }
    .hero {
      display: grid;
      gap: 10px;
      margin-bottom: 24px;
    }
    .hero h1 {
      margin: 0;
      font-size: 34px;
      line-height: 1.1;
    }
    .hero p {
      margin: 0;
      color: var(--muted);
      line-height: 1.6;
    }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      width: fit-content;
      padding: 8px 12px;
      border-radius: 999px;
      font-size: 14px;
      font-weight: 600;
    }
    .status-idle { background: var(--brand-soft); color: var(--brand); }
    .status-ok { background: var(--ok-soft); color: var(--ok); }
    .status-error { background: var(--bad-soft); color: var(--bad); }
    .grid {
      display: grid;
      grid-template-columns: 1.1fr 0.9fr;
      gap: 18px;
    }
    .panel {
      background: rgba(255, 253, 248, 0.94);
      border: 1px solid var(--line);
      border-radius: 22px;
      padding: 20px;
      box-shadow: 0 10px 32px rgba(73, 42, 8, 0.05);
      backdrop-filter: blur(6px);
    }
    .panel h2, .panel h3 {
      margin: 0 0 14px;
    }
    .note {
      margin-bottom: 14px;
      padding: 12px 14px;
      border-radius: 16px;
      background: #fff7e8;
      color: #7c5317;
      border: 1px solid #f1d39a;
      line-height: 1.6;
    }
    form {
      display: grid;
      gap: 14px;
    }
    .field {
      display: grid;
      gap: 8px;
    }
    .field label {
      font-size: 14px;
      font-weight: 600;
    }
    input, textarea {
      width: 100%;
      border: 1px solid var(--line);
      background: #fff;
      border-radius: 14px;
      padding: 12px 14px;
      font: inherit;
      color: inherit;
    }
    textarea {
      min-height: 140px;
      resize: vertical;
    }
    .two {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 14px;
    }
    button {
      border: 0;
      border-radius: 16px;
      padding: 14px 18px;
      background: linear-gradient(135deg, #0f6fff 0%, #1f8bff 100%);
      color: white;
      font: inherit;
      font-weight: 700;
      cursor: pointer;
    }
    pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12px;
      line-height: 1.65;
      background: #fff;
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 12px;
    }
    table {
      width: 100%;
      border-collapse: collapse;
    }
    td {
      vertical-align: top;
      border-top: 1px solid var(--line);
      padding: 10px 0;
      font-size: 13px;
    }
    td:first-child {
      width: 130px;
      color: var(--muted);
      padding-right: 12px;
    }
    .error {
      color: var(--bad);
      font-weight: 600;
      margin: 0 0 12px;
    }
    @media (max-width: 960px) {
      .grid { grid-template-columns: 1fr; }
      .two { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <div class="status ` + statusClass + `">` + statusText + `</div>
      <h1>支付宝当面付联调页</h1>
      <p>这个页面直接调用当前仓库里的支付宝当面付签名与下单逻辑。你填入临时参数后，只会在本次请求中使用，不会覆盖正式配置。</p>
    </section>
    <section class="grid">
      <div class="panel">
        <h2>联调参数</h2>
        <div class="note">建议先填真实生产环境的 AppID、应用私钥、支付宝公钥。如果你只是想排签名问题，notify_url 先填你当前线上回调地址即可。</div>
        <form method="post">
          <div class="two">
            <div class="field">
              <label>AppID</label>
              <input name="app_id" value="` + html.EscapeString(data.AppID) + `" />
            </div>
            <div class="field">
              <label>PID / Seller ID（可选）</label>
              <input name="seller_id" value="` + html.EscapeString(data.SellerID) + `" />
            </div>
          </div>
          <div class="field">
            <label>网关地址</label>
            <input name="gateway" value="` + html.EscapeString(data.Gateway) + `" />
          </div>
          <div class="field">
            <label>notify_url</label>
            <input name="notify_url" value="` + html.EscapeString(data.NotifyURL) + `" placeholder="https://your-domain.com/api/alipay-f2f/notify" />
          </div>
          <div class="two">
            <div class="field">
              <label>金额</label>
              <input name="amount" value="` + html.EscapeString(data.Amount) + `" />
            </div>
            <div class="field">
              <label>超时</label>
              <input name="timeout_express" value="` + html.EscapeString(data.TimeoutExpress) + `" />
            </div>
          </div>
          <div class="field">
            <label>商品标题</label>
            <input name="subject" value="` + html.EscapeString(data.Subject) + `" />
          </div>
          <div class="field">
            <label>订单号</label>
            <input name="out_trade_no" value="` + html.EscapeString(data.OutTradeNo) + `" />
          </div>
          <div class="field">
            <label>应用私钥</label>
            <textarea name="private_key">` + html.EscapeString(data.PrivateKey) + `</textarea>
          </div>
          <div class="field">
            <label>支付宝公钥</label>
            <textarea name="public_key">` + html.EscapeString(data.PublicKey) + `</textarea>
          </div>
          <button type="submit">测试当面付下单</button>
        </form>
      </div>
      <div style="display:grid; gap:18px;">
        <div class="panel">
          <h2>测试结果</h2>` + func() string {
		if strings.TrimSpace(data.ErrorMessage) == "" {
			return `<p style="margin:0;color:var(--muted);line-height:1.7;">提交后，这里会显示二维码或支付宝返回的原始错误。</p>`
		}
		return `<p class="error">` + html.EscapeString(data.ErrorMessage) + `</p>`
	}() + qrBlock + `
        </div>
        <div class="panel">
          <h3>签名原文 sign_content</h3>
          <pre>` + html.EscapeString(data.SignContent) + `</pre>
        </div>
        <div class="panel">
          <h3>请求参数</h3>
          <table>` + requestValuesBuilder.String() + `</table>
        </div>
        <div class="panel">
          <h3>原始请求体</h3>
          <pre>` + html.EscapeString(data.RawRequestBody) + `</pre>
        </div>
        <div class="panel">
          <h3>支付宝原始响应</h3>
          <pre>` + html.EscapeString(data.RawResponseBody) + `</pre>
        </div>
      </div>
    </section>
  </main>
</body>
</html>`
}
