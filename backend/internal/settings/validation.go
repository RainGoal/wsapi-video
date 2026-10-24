package settings

import (
	"net"
	"net/mail"
	"net/url"
	"strings"

	"wsapi-video/internal/payments"
)

func validOrigin(value string) bool {
	u, e := url.Parse(value)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}
func validAddress(v string) bool {
	a, e := mail.ParseAddress(v)
	return e == nil && a.Address == v && !strings.ContainsAny(v, "\r\n")
}
func (s *Service) validate(v Settings) FieldErrors {
	e := FieldErrors{}
	if n := len([]rune(strings.TrimSpace(v.Site.Brand))); n < 1 || n > 80 {
		e["site.brand"] = "站点名称须为 1–80 字"
	}
	if v.Site.SupportEmail != "" && !validAddress(v.Site.SupportEmail) {
		e["site.support_email"] = "请输入有效联系邮箱"
	}
	for field, value := range map[string]string{
		"site.retention_policy":           v.Site.RetentionPolicy,
		"site.storage_quota_policy":       v.Site.StorageQuotaPolicy,
		"site.cleanup_policy":             v.Site.CleanupPolicy,
		"site.submission_recovery_policy": v.Site.SubmissionRecoveryPolicy,
		"site.refund_policy":              v.Site.RefundPolicy,
	} {
		if len([]rune(value)) > 2000 {
			e[field] = "政策说明最多 2000 字"
		}
	}
	if v.Media.Origin != "" && !validOrigin(v.Media.Origin) {
		e["media.origin"] = "必须使用固定 HTTPS Origin，不含路径、凭证或查询参数"
	}
	if len(v.Media.OutputOrigins) > 20 {
		e["media.output_origins"] = "最多 20 个输出域名"
	}
	for _, o := range v.Media.OutputOrigins {
		if !validOrigin(o) {
			e["media.output_origins"] = "输出地址必须是固定 HTTPS Origin"
		}
	}
	if v.Media.Origin == "" && (v.Media.ImageKey != "" || v.Media.VideoKey != "" || v.Media.AnalysisKey != "") {
		e["media.origin"] = "填写凭证时必须配置网关地址"
	}
	if len(v.Media.AnalysisModel) > 200 || strings.ContainsAny(v.Media.AnalysisModel, "\r\n\x00") {
		e["media.analysis_model"] = "识图模型标识最多 200 字节，不能包含控制字符"
	}
	if v.Media.AnalysisKey != "" && strings.TrimSpace(v.Media.AnalysisModel) == "" {
		e["media.analysis_model"] = "配置识图凭证时必须填写已验证支持图像输入的模型标识"
	}
	if v.Payment.Origin != "" && payments.ValidateBaseURL(v.Payment.Origin, false) != nil {
		e["payment.origin"] = "支付网关须为 HTTPS 基础地址，可含 /epay/ 等路径前缀，不含凭证、查询参数或片段"
	}
	if len(v.Payment.PID) > 128 || strings.ContainsAny(v.Payment.PID, "&=\r\n") {
		e["payment.pid"] = "商户 ID 格式不正确"
	}
	for name, value := range map[string]string{"payment.notify_url": v.Payment.NotifyURL, "payment.return_url": v.Payment.ReturnURL} {
		u, err := url.Parse(value)
		b, _ := url.Parse(s.base.PublicURL)
		if err != nil || u == nil || u.Host != b.Host || u.Scheme != b.Scheme || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			e[name] = "回调及返回地址必须使用本站公开域名，不含查询参数"
		}
	}
	if v.Payment.NotifyURL != strings.TrimRight(s.base.PublicURL, "/")+"/api/v1/payment/notify/easypay" {
		e["payment.notify_url"] = "通知地址必须指向本站易支付通知接口"
	}
	if v.Payment.ReturnURL != strings.TrimRight(s.base.PublicURL, "/")+"/wallet" {
		e["payment.return_url"] = "返回地址必须指向本站 /wallet"
	}
	if v.Payment.Enabled && (v.Payment.Origin == "" || v.Payment.PID == "" || v.Payment.Key == "") {
		e["payment.enabled"] = "启用支付前须填写接口、商户 ID 和商户密钥"
	}
	if strings.ContainsAny(v.SMTP.Host, "/:@?\r\n ") && net.ParseIP(v.SMTP.Host) == nil {
		e["smtp.host"] = "填写 SMTP 主机名，不含协议和端口"
	}
	if v.SMTP.Port < 1 || v.SMTP.Port > 65535 {
		e["smtp.port"] = "端口必须为 1–65535"
	}
	if v.SMTP.From != "" && !validAddress(v.SMTP.From) {
		e["smtp.from"] = "填写有效发信邮箱"
	}
	if v.SMTP.Host != "" && v.SMTP.From == "" {
		e["smtp.from"] = "配置 SMTP 后必须填写发信邮箱"
	}
	if len(v.SMTP.Username) > 320 || strings.ContainsAny(v.SMTP.Username, "\r\n") {
		e["smtp.username"] = "用户名格式不正确"
	}
	if v.Storage.Driver != "s3" && v.Storage.Driver != "local" {
		e["storage.driver"] = "仅支持 local 或 s3"
	}
	if !s.base.DevMode && v.Storage.Driver != "s3" {
		e["storage.driver"] = "生产环境必须使用私有 S3/R2 存储"
	}
	if v.Storage.Endpoint != "" && !validOrigin(v.Storage.Endpoint) {
		e["storage.endpoint"] = "S3 Endpoint 必须是固定 HTTPS Origin"
	}
	if v.Storage.Driver == "s3" && (v.Storage.AccessKey != "" || v.Storage.SecretKey != "" || v.Storage.Bucket != "") && (v.Storage.AccessKey == "" || v.Storage.SecretKey == "" || v.Storage.Bucket == "" || v.Storage.Region == "") {
		e["storage.bucket"] = "S3 配置须包含桶、区域和两项访问凭证"
	}
	if len(v.Storage.Bucket) > 255 || strings.ContainsAny(v.Storage.Bucket, "/\\\r\n ") {
		e["storage.bucket"] = "桶名格式不正确"
	}
	for name, p := range secretFields(&v) {
		if len(*p) > 8192 || strings.ContainsAny(*p, "\r\n\x00") {
			e[name] = "凭证格式或长度不正确"
		}
	}
	return e
}
