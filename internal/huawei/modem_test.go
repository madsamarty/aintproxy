package huawei

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	got := passwordHash("admin", "admin", "1234")
	want := "M2IzNWMxMTA0ODU0ZTU5ZjFhMjA4YmQ5NTJhMDQ3YzkyODhlMzNiYTEyYTQ4YTkxNGZiZjY0MGRmMzY0NTZiZQ=="
	if got != want {
		t.Errorf("passwordHash() = %q, want %q", got, want)
	}
}

func TestIsOK(t *testing.T) {
	tests := []struct {
		body string
		want bool
	}{
		{"<response>OK</response>", true},
		{"<response>ERROR</response>", false},
		{"OK", true},
		{"ERROR", false},
	}
	for _, tt := range tests {
		if got := isOK(tt.body); got != tt.want {
			t.Errorf("isOK(%q) = %v, want %v", tt.body, got, tt.want)
		}
	}
}

func TestSessionTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<response><SesInfo>cookie1</SesInfo><TokInfo>tok1</TokInfo></response>`))
	}))
	defer srv.Close()

	m := New(strings.TrimPrefix(srv.URL, "http://"), "admin", "pass")
	cookie, token, err := m.sessionTokens()
	if err != nil {
		t.Fatalf("sessionTokens() error = %v", err)
	}
	if cookie != "cookie1" {
		t.Errorf("cookie = %q, want %q", cookie, "cookie1")
	}
	if token != "tok1" {
		t.Errorf("token = %q, want %q", token, "tok1")
	}
}

func TestLoginSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/webserver/SesTokInfo":
			w.Write([]byte(`<response><SesInfo>cookie1</SesInfo><TokInfo>tok1</TokInfo></response>`))
		case r.URL.Path == "/api/user/login":
			b := make([]byte, 1024)
			n, _ := r.Body.Read(b)
			s := string(b[:n])
			if !strings.Contains(s, "<Username>admin</Username>") {
				t.Errorf("expected Username in payload, got: %s", s)
			}
			w.Header().Set("Set-Cookie", "cookie2; Path=/")
			w.Header().Set("__RequestVerificationTokenOne", "tok2")
			w.Write([]byte("OK"))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	m := New(strings.TrimPrefix(srv.URL, "http://"), "admin", "admin")
	if err := m.Login(); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if m.cookie != "cookie2" {
		t.Errorf("cookie = %q, want %q", m.cookie, "cookie2")
	}
	if m.token != "tok2" {
		t.Errorf("token = %q, want %q", m.token, "tok2")
	}
}

func TestLoginFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/webserver/SesTokInfo":
			w.Write([]byte(`<response><SesInfo>c</SesInfo><TokInfo>t</TokInfo></response>`))
		case r.URL.Path == "/api/user/login":
			w.Write([]byte(`<response>ERROR</response>`))
		}
	}))
	defer srv.Close()

	m := New(strings.TrimPrefix(srv.URL, "http://"), "admin", "wrong")
	err := m.Login()
	if err == nil {
		t.Fatal("expected login error")
	}
	var me *ModemError
	if !strings.Contains(err.Error(), "login") {
		t.Errorf("error should mention login, got: %v", err)
	}
	_ = me
}

func TestRebootSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/webserver/SesTokInfo":
			w.Write([]byte(`<response><SesInfo>c</SesInfo><TokInfo>t</TokInfo></response>`))
		case r.URL.Path == "/api/user/login":
			w.Header().Set("__RequestVerificationTokenOne", "tok2")
			w.Write([]byte("OK"))
		case r.URL.Path == "/api/device/control":
			body := make([]byte, 256)
			n, _ := r.Body.Read(body)
			if !strings.Contains(string(body[:n]), "<Control>1</Control>") {
				t.Errorf("expected Control command, got: %s", string(body[:n]))
			}
			w.Write([]byte("OK"))
		}
	}))
	defer srv.Close()

	m := New(strings.TrimPrefix(srv.URL, "http://"), "admin", "admin")
	if err := m.Reboot(); err != nil {
		t.Fatalf("Reboot() error = %v", err)
	}
}

func TestSetMobileData(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/webserver/SesTokInfo":
			w.Write([]byte(`<response><SesInfo>c</SesInfo><TokInfo>t</TokInfo></response>`))
		case r.URL.Path == "/api/user/login":
			w.Header().Set("__RequestVerificationTokenOne", "tok2")
			w.Write([]byte("OK"))
		case r.URL.Path == "/api/dialup/mobile-dataswitch":
			body := make([]byte, 256)
			n, _ := r.Body.Read(body)
			calls = append(calls, string(body[:n]))
			w.Write([]byte("OK"))
		}
	}))
	defer srv.Close()

	m := New(strings.TrimPrefix(srv.URL, "http://"), "admin", "admin")
	if err := m.SetMobileData(false); err != nil {
		t.Fatalf("SetMobileData(false) error = %v", err)
	}
	if err := m.SetMobileData(true); err != nil {
		t.Fatalf("SetMobileData(true) error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if !strings.Contains(calls[0], "<dataswitch>0</dataswitch>") {
		t.Errorf("call 0: expected dataswitch 0, got %s", calls[0])
	}
	if !strings.Contains(calls[1], "<dataswitch>1</dataswitch>") {
		t.Errorf("call 1: expected dataswitch 1, got %s", calls[1])
	}
}
