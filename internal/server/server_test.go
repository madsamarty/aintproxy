package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mohamed-sameh/aintproxy/internal/config"
	"github.com/mohamed-sameh/aintproxy/internal/rotation"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testConfig(port int) *config.Config {
	return &config.Config{
		Modem: config.ModemConfig{
			IP:       "192.168.8.1",
			Password: "pw",
		},
		Network: config.NetworkConfig{
			IPCheckURL: "http://127.0.0.1:1",
			UseSudo:    false,
		},
		Rotation: config.RotationConfig{
			RebootWait: 0,
		},
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: port,
		},
	}
}

func testServer(cfg *config.Config, rot Rotator) *Server {
	s := &Server{
		config:    cfg,
		logger:    testLogger(),
		rotator:   rot,
		startedAt: time.Now(),
		history:   NewHistory(100),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /rotate", s.handleRotate)
	mux.HandleFunc("POST /hard-rotate", s.handleHardRotate)
	mux.HandleFunc("GET /info", s.handleInfo)
	mux.HandleFunc("GET /history", s.handleHistory)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	s.http = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: mux,
	}

	return s
}

type mockRotator struct {
	oldIP  string
	newIP  string
	err    error
	info   *rotation.InfoResult
}

func (m *mockRotator) Rotate() (string, string, error) {
	return m.oldIP, m.newIP, m.err
}

func (m *mockRotator) HardRotate() ([]rotation.RotateResult, error) {
	return []rotation.RotateResult{{
		OldIP: m.oldIP,
		NewIP: m.newIP,
		Err:   m.err,
	}}, nil
}

func (m *mockRotator) Info() (*rotation.InfoResult, error) {
	if m.info != nil {
		return m.info, nil
	}
	return &rotation.InfoResult{
		PublicIP:  m.oldIP,
		Interface: "vodafone0",
		ModemIP:   "192.168.8.1",
	}, nil
}

func TestHealthEndpoint(t *testing.T) {
	cfg := testConfig(18080)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s:%d/health", cfg.Server.Host, cfg.Server.Port))
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["uptime"] == nil || body["uptime"] == "" {
		t.Error("expected non-empty uptime in health response")
	}
	if body["started_at"] == nil || body["started_at"] == "" {
		t.Error("expected non-empty started_at in health response")
	}
	if body["history_size"] == nil {
		t.Error("expected history_size in health response")
	}
}

func TestRotateEndpoint(t *testing.T) {
	cfg := testConfig(18081)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	resp, err := http.Post(base+"/rotate", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("POST /rotate error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 200, body: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["old_ip"] != "1.1.1.1" {
		t.Errorf("old_ip = %v, want 1.1.1.1", result["old_ip"])
	}
	if result["new_ip"] != "2.2.2.2" {
		t.Errorf("new_ip = %v, want 2.2.2.2", result["new_ip"])
	}
	if result["rotated"] != true {
		t.Errorf("rotated = %v, want true", result["rotated"])
	}
}

func TestRotateRequiresAuth(t *testing.T) {
	cfg := testConfig(18082)
	cfg.Server.AuthToken = "sekret"
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)

	// No token -> 401
	resp, _ := http.Post(base+"/rotate", "application/json", bytes.NewReader([]byte("{}")))
	if resp.StatusCode != 401 {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Wrong token -> 401
	req, _ := http.NewRequest("POST", base+"/rotate", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Auth-Token", "wrong")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 401 {
		t.Errorf("wrong token: status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Correct token -> 200
	req, _ = http.NewRequest("POST", base+"/rotate", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Auth-Token", "sekret")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Errorf("correct token: status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestRotateFailureReturns500(t *testing.T) {
	cfg := testConfig(18084)
	s := testServer(cfg, &mockRotator{err: fmt.Errorf("modem on fire")})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	resp, _ := http.Post(base+"/rotate", "application/json", bytes.NewReader([]byte("{}")))
	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "modem on fire" {
		t.Errorf("error = %q, want %q", body["error"], "modem on fire")
	}
	resp.Body.Close()
}

func TestUnknownRouteReturns404(t *testing.T) {
	cfg := testConfig(18085)
	s := testServer(cfg, &mockRotator{})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	resp, err := http.Get(base + "/nope")
	if err != nil {
		t.Fatalf("GET /nope error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRateLimiting(t *testing.T) {
	cfg := testConfig(18086)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)

	// First rotate succeeds
	resp, _ := http.Post(base+"/rotate", "application/json", bytes.NewReader([]byte("{}")))
	if resp.StatusCode != 200 {
		t.Errorf("first rotate: status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Second rotate within 30s is rate limited
	resp, _ = http.Post(base+"/rotate", "application/json", bytes.NewReader([]byte("{}")))
	if resp.StatusCode != 429 {
		t.Errorf("second rotate: status = %d, want 429", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "rate limited" {
		t.Errorf("error = %v, want 'rate limited'", body["error"])
	}
	resp.Body.Close()
}

func TestInfoEndpoint(t *testing.T) {
	cfg := testConfig(18087)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	resp, err := http.Get(base + "/info")
	if err != nil {
		t.Fatalf("GET /info error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var info rotation.InfoResult
	json.NewDecoder(resp.Body).Decode(&info)
	if info.PublicIP != "1.1.1.1" {
		t.Errorf("public_ip = %q, want 1.1.1.1", info.PublicIP)
	}
	if info.Interface != "vodafone0" {
		t.Errorf("interface = %q, want vodafone0", info.Interface)
	}
}

func TestHistoryEndpoint(t *testing.T) {
	cfg := testConfig(18088)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)

	// Do a rotation
	resp, _ := http.Post(base+"/rotate", "application/json", bytes.NewReader([]byte("{}")))
	resp.Body.Close()

	// Check history
	resp, err := http.Get(base + "/history")
	if err != nil {
		t.Fatalf("GET /history error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	total := int(result["total"].(float64))
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
}

func TestHardRotateEndpoint(t *testing.T) {
	cfg := testConfig(18089)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "3.3.3.3"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	resp, err := http.Post(base+"/hard-rotate", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("POST /hard-rotate error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	cfg := testConfig(18090)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	resp, err := http.Get(base + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("aintproxy_rotations_total")) {
		t.Error("expected aintproxy_rotations_total in metrics")
	}
	if !bytes.Contains(body, []byte("aintproxy_uptime_seconds")) {
		t.Error("expected aintproxy_uptime_seconds in metrics")
	}
}
