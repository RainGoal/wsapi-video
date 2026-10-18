package settings

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"time"

	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/storage"
)

func (s *Service) test(w http.ResponseWriter, r *http.Request) {
	scope := r.PathValue("scope")
	if scope != "media" && scope != "payment" && scope != "smtp" && scope != "storage" {
		httpx.Fail(w, 404, "not_found", "不存在此配置检测")
		return
	}
	var in struct{}
	if !httpx.Decode(w, r, &in) {
		return
	}
	st, err := s.Current(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "settings_unavailable", "无法读取配置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	message := ""
	ok := false
	switch scope {
	case "media":
		m := st.Settings.Media
		if m.Origin == "" || m.ImageKey == "" && m.VideoKey == "" {
			message = "请先保存网关地址和凭证"
			break
		}
		ok = true
		seen := map[string]bool{}
		for _, key := range []string{m.ImageKey, m.VideoKey} {
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			req, buildErr := http.NewRequestWithContext(ctx, http.MethodGet, m.Origin+"/v1/models", nil)
			if buildErr != nil {
				ok = false
				break
			}
			req.Header.Set("Authorization", "Bearer "+key)
			if !probeHTTP(req, true) {
				ok = false
				break
			}
		}
		if ok {
			message = "已填写的生成凭证均通过只读模型列表检查；具体图片/视频能力仍须逐商品验证"
		} else {
			message = "网关连接或凭证验证失败，请检查地址、TLS 与密钥"
		}
	case "payment":
		p := st.Settings.Payment
		if p.Origin == "" || p.PID == "" || p.Key == "" {
			message = "请先保存支付地址、商户 ID 与商户密钥"
			break
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodHead, p.Origin+"/submit.php", nil)
		ok = probeHTTP(req, false)
		if ok {
			message = "支付 HTTPS 接口可达；此检测不验证商户密钥和到账回调，正式开放前仍须商户联调"
		} else {
			message = "支付 HTTPS 接口不可达，请检查地址与 TLS"
		}
	case "smtp":
		if st.Settings.SMTP.Host == "" {
			message = "请先保存 SMTP 配置"
			break
		}
		err = probeSMTP(ctx, st.Settings.SMTP)
		ok = err == nil
		if ok {
			message = "SMTP 连接、TLS 与认证成功，未发送邮件"
		} else {
			message = "SMTP 连接、TLS 或认证失败，请检查已保存配置"
		}
	case "storage":
		if !s.Ready(st).Storage {
			message = "请先完整保存存储配置"
			break
		}
		err = storage.Check(ctx, s.Config(st))
		ok = err == nil
		if ok {
			message = "私有存储位置可访问；此只读检测未写入或删除对象，发布前仍须验证上传和下载权限"
		} else {
			message = "存储连接或访问授权失败，请检查 Endpoint、桶、区域与凭证"
		}
	}
	details, _ := json.Marshal(map[string]any{"scope": scope, "ok": ok, "revision": st.Revision})
	_, err = s.db.Exec(r.Context(), "INSERT INTO admin_audit(id,admin_id,action,target_id,reason,details) VALUES($1,$2,'settings.test',$3,'管理员执行只读连接检测',$4)", core.ID("audit"), core.UserFrom(r.Context()).ID, scope, details)
	if err != nil {
		httpx.Fail(w, 503, "audit_unavailable", "检测已结束，但审计写入失败，请重试")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": ok, "message": message, "scope": scope, "checked_at": time.Now().UTC()})
}
func probeHTTP(req *http.Request, requireOK bool) bool {
	if req == nil {
		return false
	}
	c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if requireOK {
		return resp.StatusCode == 200
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 500 && resp.StatusCode != 404
}
func probeSMTP(ctx context.Context, v SMTP) error {
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(v.Host, strconv.Itoa(v.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = conn.SetDeadline(deadline)
	if v.Port == 465 {
		tc := tls.Client(conn, &tls.Config{ServerName: v.Host, MinVersion: tls.VersionTLS12})
		if err = tc.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, v.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if v.Port != 465 {
		if yes, _ := c.Extension("STARTTLS"); !yes {
			return errors.New("TLS is required")
		}
		if err = c.StartTLS(&tls.Config{ServerName: v.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if v.Username != "" {
		if err = c.Auth(smtp.PlainAuth("", v.Username, v.Password, v.Host)); err != nil {
			return err
		}
	}
	return c.Quit()
}
