package zte

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeHTMLEntities(t *testing.T) {
	tests := []struct {
		name string
		input string
		want  string
	}{
		{
			name:  "ZXHN decimal entities",
			input: "&#90;&#88;&#72;&#78; ZXHN H188A",
			want:  "ZXHN ZXHN H188A",
		},
		{
			name:  "plain text",
			input: "Hello World",
			want:  "Hello World",
		},
		{
			name:  "mixed",
			input: "Model: &#90;&#88;&#72;&#78; H188A",
			want:  "Model: ZXHN H188A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeHTMLEntities(tt.input)
			if got != tt.want {
				t.Errorf("decodeHTMLEntities(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecodeNumericEntity(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  rune
	}{
		{"decimal 90", "90", 'Z'},
		{"decimal 88", "88", 'X'},
		{"hex 5A", "x5A", 'Z'},
		{"hex 58", "x58", 'X'},
		{"empty", "", 0},
		{"invalid", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeNumericEntity(tt.input)
			if got != tt.want {
				t.Errorf("decodeNumericEntity(%q) = %c (%d), want %c (%d)", tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestParseTitle(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		wantOK    bool
		wantModel string
	}{
		{
			name:      "ZTE with decimal entities",
			html:      `<html><head><title>&#90;&#88;&#72;&#78; ZXHN H188A</title></head></html>`,
			wantOK:    true,
			wantModel: "ZXHN ZXHN H188A",
		},
		{
			name:   "no title",
			html:   "<html><head></head></html>",
			wantOK: false,
		},
		{
			name:   "not ZTE",
			html:   "<html><head><title>Some Other Router</title></head></html>",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOK, gotModel := parseTitle(tt.html)
			if gotOK != tt.wantOK {
				t.Errorf("parseTitle() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if tt.wantOK && gotModel != tt.wantModel {
				t.Errorf("parseTitle() model = %q, want %q", gotModel, tt.wantModel)
			}
		})
	}
}

func TestDetectWithServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>&#90;&#88;&#72;&#78; ZXHN H188A</title></head></html>`))
	}))
	defer srv.Close()

	ip := strings.TrimPrefix(srv.URL, "https://")
	if !detect(ip, "admin", "pass") {
		t.Error("detect() returned false for ZTE modem")
	}
}

func TestDetectModelWithServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>&#90;&#88;&#72;&#78; ZXHN H188A</title></head></html>`))
	}))
	defer srv.Close()

	ip := strings.TrimPrefix(srv.URL, "https://")
	ok, model := detectModel(ip)
	if !ok {
		t.Error("detectModel() returned false")
	}
	if !strings.Contains(model, "ZXHN") {
		t.Errorf("detectModel() model = %q, should contain ZXHN", model)
	}
}

func TestRebootNotImplemented(t *testing.T) {
	m := New("192.168.1.1", "admin", "pass")
	err := m.Reboot()
	if err == nil {
		t.Error("expected error for unimplemented Reboot()")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error should mention 'not yet implemented', got: %v", err)
	}
}

func TestSetMobileDataNotImplemented(t *testing.T) {
	m := New("192.168.1.1", "admin", "pass")
	err := m.SetMobileData(false)
	if err == nil {
		t.Error("expected error for unimplemented SetMobileData()")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error should mention 'not yet implemented', got: %v", err)
	}
}
