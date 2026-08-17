package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mohamed-sameh/aintproxy/internal/config"
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
		config:  cfg,
		logger:  testLogger(),
		rotator: rot,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /rotate", s.handleRotate)

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
}

func (m *mockRotator) Rotate() (string, string, error) {
	return m.oldIP, m.newIP, m.err
}

func TestHealthEndpoint(t *testing.T) {
	cfg := testConfig(18080)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(nil)
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s:%d/health", cfg.Server.Host, cfg.Server.Port))
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestRotateEndpoint(t *testing.T) {
	cfg := testConfig(18081)
	s := testServer(cfg, &mockRotator{oldIP: "1.1.1.1", newIP: "2.2.2.2"})

	go s.Start()
	defer s.Shutdown(nil)
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
	defer s.Shutdown(nil)
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
	defer s.Shutdown(nil)
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
	defer s.Shutdown(nil)
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
