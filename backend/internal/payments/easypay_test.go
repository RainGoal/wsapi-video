package payments

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGatewayBasePaths(t *testing.T) {
	for _, prefix := range []string{"", "/", "/epay", "/epay/", "/gateway/epay/"} {
		t.Run(prefix, func(t *testing.T) {
			wantPath := strings.TrimRight(prefix, "/")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != wantPath+"/api.php" {
					t.Errorf("wrong query endpoint: %s %s", r.Method, r.URL.Path)
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				for k, want := range map[string]string{"act": "order", "pid": "1001", "key": "secret", "out_trade_no": "CV123"} {
					if r.URL.Query().Get(k) != want || r.PostForm.Get(k) != want {
						t.Errorf("query and form must both contain %s", k)
					}
				}
				_, _ = io.WriteString(w, `{"code":1,"out_trade_no":"CV123","pid":"1001","status":1,"money":"10.01","trade_no":"provider-1"}`)
			}))
			defer srv.Close()
			e, err := New(Config{Origin: srv.URL + prefix, PID: "1001", Key: "secret", CredentialRef: "v1", NotifyURL: srv.URL + "/notify", ReturnURL: srv.URL + "/wallet", AllowHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			checkout, err := e.Checkout("CV123", "alipay", 1001)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(checkout)
			if err != nil || u.Path != wantPath+"/submit.php" {
				t.Fatalf("wrong checkout endpoint: %s, %v", checkout, err)
			}
			params := map[string]string{}
			for k, values := range u.Query() {
				params[k] = values[0]
			}
			if params["money"] != "10.01" || params["pid"] != "1001" || params["out_trade_no"] != "CV123" || params["sign"] != Sign(params, "secret") {
				t.Fatal("checkout identity, amount or signature changed")
			}
			n, err := e.Verify(signedNotify())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.Confirm(context.Background(), n); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGatewayURLValidation(t *testing.T) {
	for _, origin := range []string{"https://pay.example", "https://pay.example/", "https://pay.example/epay", "https://pay.example/epay/", "https://pay.example/gateway/epay/", "https://api.kaipay.cn/epay/"} {
		cfg := Config{Origin: origin, PID: "1001", Key: "secret", CredentialRef: "v1", NotifyURL: "https://app.example/notify", ReturnURL: "https://app.example/wallet"}
		if _, err := New(cfg); err != nil {
			t.Errorf("valid HTTPS gateway rejected: %s: %v", origin, err)
		}
	}
	for _, origin := range []string{
		"", "pay.example/epay", "http://pay.example/epay", "//pay.example/epay", "https:///epay", "https://:443/epay",
		"https://user:password@pay.example/epay", "https://pay.example/epay?key=secret", "https://pay.example/epay?",
		"https://pay.example/epay#fragment", "https://pay.example/epay#", "https://pay.example/epay\\other",
		"https://pay.example/./epay", "https://pay.example/epay/../other", "https://pay.example/epay/.",
		"https://pay.example//epay", "https://pay.example/epay//", "https://pay.example//",
		"https://pay.example/epay/%2e%2e/other", "https://pay.example/epay%2fother", "https://pay.example/epay%5cother",
		"https://pay.example/epay/%252e%252e/other", "https://pay.example/epay%0a", "https://pay.example/epay\n",
	} {
		t.Run(origin, func(t *testing.T) {
			cfg := Config{Origin: origin, PID: "1001", Key: "secret", CredentialRef: "v1", NotifyURL: "https://app.example/notify", ReturnURL: "https://app.example/wallet"}
			if _, err := New(cfg); err == nil {
				t.Fatal("invalid gateway accepted")
			}
		})
	}
}

func signedNotify() url.Values {
	p := map[string]string{"pid": "1001", "out_trade_no": "CV123", "trade_no": "provider-1", "money": "10.01", "trade_status": "TRADE_SUCCESS", "sign_type": "MD5"}
	p["sign"] = Sign(p, "secret")
	v := url.Values{}
	for k, s := range p {
		v.Set(k, s)
	}
	return v
}
func TestExactMoneyAndNotification(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "10": 1000, "10.01": 1001, "10.1": 1010, "99999999.99": 9999999999} {
		v, e := ParseFen(in)
		if e != nil || v != want {
			t.Fatalf("%q=%d,%v", in, v, e)
		}
		if v2, e := ParseFen(FormatFen(v)); e != nil || v2 != v {
			t.Fatal("money roundtrip")
		}
	}
	for _, in := range []string{"-1", "1.001", "1e2", "NaN", " 1", "+1", "1.", "1.0x"} {
		if _, e := ParseFen(in); e == nil {
			t.Fatalf("invalid money accepted: %q", in)
		}
	}
	e, _ := New(Config{Origin: "https://pay.example", PID: "1001", Key: "secret", CredentialRef: "v1", NotifyURL: "https://app.example/notify", ReturnURL: "https://app.example/wallet"})
	v := signedNotify()
	n, err := e.Verify(v)
	if err != nil || !n.Paid || n.AmountFen != 1001 {
		t.Fatal(n, err)
	}
	v.Add("money", "10.01")
	if _, err = e.Verify(v); err == nil {
		t.Fatal("duplicate payment amount accepted")
	}
	v = signedNotify()
	v.Set("return_url", "https://attacker.example")
	if _, err = e.Verify(v); err == nil {
		t.Fatal("checkout signature reuse accepted")
	}
	v = signedNotify()
	v.Set("pid", "other")
	if _, err = e.Verify(v); err == nil {
		t.Fatal("wrong merchant accepted")
	}
}
func TestQueryConfirmationRejectsContradictions(t *testing.T) {
	response := `{"code":1,"out_trade_no":"CV123","pid":"1001","status":1,"money":"10.01","trade_no":"provider-1"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.php" || r.URL.Query().Get("key") != "secret" {
			t.Error("wrong fixed payment query")
		}
		_, _ = io.WriteString(w, response)
	}))
	defer srv.Close()
	e, err := New(Config{Origin: srv.URL, PID: "1001", Key: "secret", CredentialRef: "v1", NotifyURL: srv.URL + "/notify", ReturnURL: srv.URL + "/wallet", AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := e.Verify(signedNotify())
	if _, err = e.Confirm(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"code":-1,"status":1,"money":"10.01"}`, `{"code":1,"trade_status":"WAITING","status":1,"money":"10.01"}`, `{"code":1,"out_trade_no":"other","status":1,"money":"10.01"}`, `{"code":1,"status":1,"money":"10.02"}`, `{"code":1,"status":1,"money":"10.01","trade_no":"other"}`} {
		response = bad
		if _, err = e.Confirm(context.Background(), n); err == nil {
			t.Fatalf("contradictory paid query accepted: %s", bad)
		}
	}
}
