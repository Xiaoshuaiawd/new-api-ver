package setting

var (
	AlipayF2FEnabled       bool
	AlipayF2FAppID         string
	AlipayF2FSellerID      string
	AlipayF2FPrivateKey    string
	AlipayF2FPublicKey     string
	AlipayF2FGateway       = "https://openapi.alipay.com/gateway.do"
	AlipayF2FDisplayName   = "支付宝当面付"
	AlipayF2FMinTopUp      = 1
	AlipayF2FOrderTimeout  = 30
	AlipayF2FSubjectPrefix = "new-api"
)
