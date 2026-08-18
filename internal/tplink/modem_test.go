package tplink

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseTitle(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		wantOK   bool
		wantModel string
	}{
		{
			name:     "TP-LINK MR600",
			html:     "<html><head><title>TP-LINK Archer MR600</title></head></html>",
			wantOK:   true,
			wantModel: "TP-LINK Archer MR600",
		},
		{
			name:     "TD-W8968",
			html:     "<html><head><title>TD-W8968 - TP-LINK</title></head></html>",
			wantOK:   true,
			wantModel: "TD-W8968 - TP-LINK",
		},
		{
			name:   "no title",
			html:   "<html><head></head></html>",
			wantOK: false,
		},
		{
			name:   "not tp-link",
			html:   "<html><head><title>Some Other Router</title></head></html>",
			wantOK: false,
		},
		{
			name:     "archer in title",
			html:     "<html><head><title>Archer AX73</title></head></html>",
			wantOK:   true,
			wantModel: "Archer AX73",
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>TP-LINK Archer MR600</title></head></html>`))
	}))
	defer srv.Close()

	ip := strings.TrimPrefix(srv.URL, "http://")
	if !detect(ip, "admin", "pass") {
		t.Error("detect() returned false for TP-Link modem")
	}
}

func TestDetectModelWithServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>TP-LINK Archer MR600</title></head></html>`))
	}))
	defer srv.Close()

	ip := strings.TrimPrefix(srv.URL, "http://")
	ok, model := detectModel(ip)
	if !ok {
		t.Error("detectModel() returned false")
	}
	if model != "TP-LINK Archer MR600" {
		t.Errorf("detectModel() model = %q, want %q", model, "TP-LINK Archer MR600")
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
	err := m.SetMobileData(true)
	if err == nil {
		t.Error("expected error for unimplemented SetMobileData()")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error should mention 'not yet implemented', got: %v", err)
	}
}
