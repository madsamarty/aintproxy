package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.Modem.IP != "192.168.8.1" {
		t.Errorf("modem ip = %q, want %q", cfg.Modem.IP, "192.168.8.1")
	}
	if cfg.Server.Port != 5000 {
		t.Errorf("server port = %d, want 5000", cfg.Server.Port)
	}
	if cfg.Network.ProxyPort != 3128 {
		t.Errorf("proxy port = %d, want 3128", cfg.Network.ProxyPort)
	}
}

func TestLoadFromFile(t *testing.T) {
	content := `
modem:
  ip: "10.0.0.1"
  user: "root"
  password: "secret"
  interface: "wwan0"
network:
  local_ip: "10.0.0.5"
  routing_table: "200"
  proxy_host: "127.0.0.1"
  proxy_port: 8080
  ip_check_url: "https://ifconfig.me"
  use_sudo: false
rotation:
  reboot_wait: 10
  data_off_wait: 15
  ip_check_attempts: 5
  ip_check_interval: 1
server:
  host: "127.0.0.1"
  port: 8000
  auth_token: "mytoken"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Modem.IP != "10.0.0.1" {
		t.Errorf("modem ip = %q, want %q", cfg.Modem.IP, "10.0.0.1")
	}
	if cfg.Modem.Password != "secret" {
		t.Errorf("modem password = %q, want %q", cfg.Modem.Password, "secret")
	}
	if cfg.Modem.Interface != "wwan0" {
		t.Errorf("modem interface = %q, want %q", cfg.Modem.Interface, "wwan0")
	}
	if cfg.Network.LocalIP != "10.0.0.5" {
		t.Errorf("network local_ip = %q, want %q", cfg.Network.LocalIP, "10.0.0.5")
	}
	if cfg.Network.ProxyPort != 8080 {
		t.Errorf("proxy port = %d, want 8080", cfg.Network.ProxyPort)
	}
	if cfg.Network.UseSudo != false {
		t.Errorf("use_sudo = %v, want false", cfg.Network.UseSudo)
	}
	if cfg.Rotation.RebootWait != 10 {
		t.Errorf("reboot wait = %d, want 10", cfg.Rotation.RebootWait)
	}
	if cfg.Server.Port != 8000 {
		t.Errorf("server port = %d, want 8000", cfg.Server.Port)
	}
	if cfg.Server.AuthToken != "mytoken" {
		t.Errorf("auth token = %q, want %q", cfg.Server.AuthToken, "mytoken")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadMissingPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nopass.yaml")
	if err := os.WriteFile(path, []byte("modem:\n  password: ''"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing password")
	}
}

func TestLoadInvalidMode(t *testing.T) {
	t.Skip("rotation.mode removed — no longer validated")
}

func TestPartialConfigUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.yaml")
	content := "modem:\n  password: secret123\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Modem.Password != "secret123" {
		t.Errorf("password = %q, want %q", cfg.Modem.Password, "secret123")
	}
	if cfg.Modem.IP != "192.168.8.1" {
		t.Errorf("modem ip should be default, got %q", cfg.Modem.IP)
	}
	if cfg.Server.Port != 5000 {
		t.Errorf("server port should be default, got %d", cfg.Server.Port)
	}
}
