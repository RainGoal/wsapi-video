// Package payments implements 彩虹易支付-compatible hosted checkout and confirmation.
// Compatibility is based on Sub2API's internal/payment/provider/easypay.go contract.
package payments

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Origin, PID, Key, NotifyURL, ReturnURL, CredentialRef string
	AllowHTTP                                             bool
}
type EasyPay struct {
	cfg    Config
	client *http.Client
}
type Confirmation struct {
	OrderNo, TradeNo string
	AmountFen        int64
	Paid             bool
}

func New(cfg Config) (*EasyPay, error) {
	for _, s := range []string{cfg.Origin, cfg.NotifyURL, cfg.ReturnURL} {
		u, e := url.Parse(s)
		if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(cfg.AllowHTTP && u.Scheme == "http")) {
			return nil, errors.New("EasyPay requires HTTPS URLs")
		}
	}
	u, _ := url.Parse(cfg.Origin)
	if u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("EasyPay origin must not contain a path or query")
	}
	if cfg.PID == "" || cfg.Key == "" || cfg.CredentialRef == "" {
		return nil, errors.New("EasyPay credentials are missing")
	}
	cfg.Origin = strings.TrimRight(cfg.Origin, "/")
	return &EasyPay{cfg: cfg, client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func Sign(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k != "sign" && k != "sign_type" && v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return hex.EncodeToString(sum[:])
}

// ParseFen avoids all floating-point rounding in money validation.
func ParseFen(s string) (int64, error) {
	if s == "" || strings.TrimSpace(s) != s {
		return 0, errors.New("invalid money")
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 10 {
		return 0, errors.New("invalid money")
	}
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid money")
		}
	}
	whole, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return 0, errors.New("invalid money")
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, errors.New("invalid money")
		}
		fraction = parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		for _, r := range fraction {
			if r < '0' || r > '9' {
				return 0, errors.New("invalid money")
			}
		}
	}
	f, _ := strconv.ParseInt(fraction, 10, 64)
	return whole*100 + f, nil
}
func FormatFen(f int64) string { return strconv.FormatInt(f/100, 10) + "." + fmtTwo(f%100) }
func fmtTwo(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}

func (e *EasyPay) Checkout(orderNo, method string, amountFen int64) (string, error) {
	if e == nil {
		return "", errors.New("EasyPay is unavailable")
	}
	if method != "alipay" && method != "wxpay" {
		return "", errors.New("unsupported payment method")
	}
	if amountFen <= 0 {
		return "", errors.New("invalid payment amount")
	}
	params := map[string]string{"pid": e.cfg.PID, "type": method, "out_trade_no": orderNo, "notify_url": e.cfg.NotifyURL, "return_url": e.cfg.ReturnURL, "name": "创作积分充值", "money": FormatFen(amountFen)}
	params["sign"] = Sign(params, e.cfg.Key)
	params["sign_type"] = "MD5"
	v := url.Values{}
	for k, s := range params {
		v.Set(k, s)
	}
	return e.cfg.Origin + "/submit.php?" + v.Encode(), nil
}

var allowedNotify = map[string]bool{"pid": true, "trade_no": true, "out_trade_no": true, "type": true, "name": true, "money": true, "trade_status": true, "param": true, "sign": true, "sign_type": true}

func (e *EasyPay) Verify(values url.Values) (Confirmation, error) {
	if e == nil {
		return Confirmation{}, errors.New("EasyPay is unavailable")
	}
	p := map[string]string{}
	for k, vs := range values {
		if !allowedNotify[k] || len(vs) != 1 {
			return Confirmation{}, errors.New("ambiguous payment notification")
		}
		v := vs[0]
		if k == "param" || k == "name" {
			for _, part := range strings.Split(v, "&")[1:] {
				field, _, ok := strings.Cut(part, "=")
				if ok && allowedNotify[field] {
					return Confirmation{}, errors.New("ambiguous payment notification")
				}
			}
		} else if strings.ContainsAny(v, "&=\r\n") {
			return Confirmation{}, errors.New("ambiguous payment notification")
		}
		p[k] = v
	}
	if p["pid"] != e.cfg.PID || (p["sign_type"] != "" && p["sign_type"] != "MD5") || !hmac.Equal([]byte(Sign(p, e.cfg.Key)), []byte(p["sign"])) {
		return Confirmation{}, errors.New("invalid payment notification signature")
	}
	f, e2 := ParseFen(p["money"])
	if e2 != nil || f <= 0 || p["out_trade_no"] == "" || p["trade_no"] == "" {
		return Confirmation{}, errors.New("invalid payment notification identity")
	}
	return Confirmation{OrderNo: p["out_trade_no"], TradeNo: p["trade_no"], AmountFen: f, Paid: p["trade_status"] == "TRADE_SUCCESS"}, nil
}

func scalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}
func pick(a, b json.RawMessage) json.RawMessage {
	if len(a) > 0 && string(a) != "null" {
		return a
	}
	return b
}

func (e *EasyPay) Query(ctx context.Context, orderNo string) (Confirmation, error) {
	if e == nil {
		return Confirmation{}, errors.New("EasyPay is unavailable")
	}
	v := url.Values{"act": {"order"}, "pid": {e.cfg.PID}, "key": {e.cfg.Key}, "out_trade_no": {orderNo}}
	// PHP forks differ on GET versus POST parameters. Credentials stay server-side.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.Origin+"/api.php?"+v.Encode(), strings.NewReader(v.Encode()))
	if err != nil {
		return Confirmation{}, errors.New("cannot build payment query")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := e.client.Do(req)
	if err != nil {
		return Confirmation{}, errors.New("payment query transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Confirmation{}, errors.New("payment query rejected")
	}
	type fields struct {
		Status      json.RawMessage `json:"status"`
		TradeStatus *string         `json:"trade_status"`
		Money       json.RawMessage `json:"money"`
		TradeNo     json.RawMessage `json:"trade_no"`
		OrderNo     json.RawMessage `json:"out_trade_no"`
		PID         json.RawMessage `json:"pid"`
	}
	var answer struct {
		fields
		Code json.RawMessage `json:"code"`
		Data fields          `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer) != nil || scalar(answer.Code) != "1" {
		return Confirmation{}, errors.New("invalid payment query result")
	}
	if pid := scalar(pick(answer.PID, answer.Data.PID)); pid != "" && pid != e.cfg.PID {
		return Confirmation{}, errors.New("payment merchant mismatch")
	}
	if no := scalar(pick(answer.OrderNo, answer.Data.OrderNo)); no != "" && no != orderNo {
		return Confirmation{}, errors.New("payment order mismatch")
	}
	paid := false
	if answer.TradeStatus != nil {
		paid = *answer.TradeStatus == "TRADE_SUCCESS"
	} else if answer.Data.TradeStatus != nil {
		paid = *answer.Data.TradeStatus == "TRADE_SUCCESS"
	} else {
		paid = scalar(pick(answer.Status, answer.Data.Status)) == "1"
	}
	f, err := ParseFen(scalar(pick(answer.Money, answer.Data.Money)))
	if err != nil {
		return Confirmation{}, errors.New("invalid payment query amount")
	}
	return Confirmation{OrderNo: orderNo, TradeNo: scalar(pick(answer.TradeNo, answer.Data.TradeNo)), AmountFen: f, Paid: paid}, nil
}

// Confirm is mandatory even after verifying the legacy MD5 notification signature.
func (e *EasyPay) Confirm(ctx context.Context, n Confirmation) (Confirmation, error) {
	q, err := e.Query(ctx, n.OrderNo)
	if err != nil {
		return Confirmation{}, err
	}
	if !q.Paid || q.AmountFen != n.AmountFen || (q.TradeNo != "" && q.TradeNo != n.TradeNo) {
		return Confirmation{}, errors.New("payment notification does not match the queried order")
	}
	if q.TradeNo == "" {
		q.TradeNo = n.TradeNo
	}
	return q, nil
}
