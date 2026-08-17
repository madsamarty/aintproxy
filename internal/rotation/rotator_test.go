package rotation

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/mohamed-sameh/aintproxy/internal/config"
	"github.com/mohamed-sameh/aintproxy/internal/modem"
)

func testConfig(overrides map[string]any) *config.Config {
	cfg := &config.Config{
		Modem: config.ModemConfig{
			IP:        "192.168.8.1",
			User:      "admin",
			Password:  "pw",
			Interface: "vodafone0",
		},
		Network: config.NetworkConfig{
			LocalIP:        "",
			RoutingTable:   "100",
			ProxyHost:      "127.0.0.1",
			ProxyPort:      3128,
			IPCheckURL:     "https://api.ipify.org",
			UseSudo:        false,
		},
		Rotation: config.RotationConfig{
			RebootWait:      0,
			ToggleWait:      0,
			IPCheckAttempts: 1,
			IPCheckInterval: 0,
		},
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: 5000,
		},
	}
	for k, v := range overrides {
		switch k {
		case "connect_attempts":
			cfg.Rotation.IPCheckAttempts = v.(int)
		}
	}
	return cfg
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testRotator(cfg *config.Config, m modem.Modem) *Rotator {
	r := &Rotator{
		Config: cfg,
		Modem:  m,
		Logger: testLogger(),
	}
	r.RunCmd = func(name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}
	return r
}

func TestCurrentIPReturnsEmptyOnError(t *testing.T) {
	cfg := testConfig(nil)
	cfg.Network.IPCheckURL = "http://192.0.2.1:1" // TEST-NET, unreachable
	r := testRotator(cfg, &fakeModem{})
	ip, err := r.CurrentIP()
	if err != nil {
		t.Errorf("CurrentIP() error = %v, want nil", err)
	}
	if ip != "" {
		t.Errorf("CurrentIP() = %q, want empty", ip)
	}
}

func TestRotateFastLeaseDrop(t *testing.T) {
	cfg := testConfig(map[string]any{
		"connect_attempts": 3,
	})

	fm := &fakeModem{}
	r := testRotator(cfg, fm)
	call := 0
	r.CurrentIPIs = func() (string, error) {
		call++
		return "1.1.1.1", nil
	}

	oldIP, newIP, err := r.Rotate()
	if err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if oldIP != "1.1.1.1" {
		t.Errorf("old IP = %q, want %q", oldIP, "1.1.1.1")
	}
	if newIP != "1.1.1.1" {
		t.Errorf("new IP = %q, want %q", newIP, "1.1.1.1")
	}
	if !fm.setMobileDataCalled {
		t.Error("expected SetMobileData to be called (toggle mode)")
	}
	if fm.rebootCalled {
		t.Error("Reboot should NOT be called for fast lease drop")
	}
}

func TestRotateFastLeaseDropFailsOnModemError(t *testing.T) {
	cfg := testConfig(nil)

	r := testRotator(cfg, &fakeModem{setMobileDataErr: fmt.Errorf("modem on fire")})
	r.CurrentIPIs = func() (string, error) { return "1.1.1.1", nil }

	_, _, err := r.Rotate()
	if err == nil {
		t.Fatal("expected error from modem failure")
	}
}

func TestRotateRebootDeepClean(t *testing.T) {
	cfg := testConfig(map[string]any{
		"connect_attempts": 3,
	})

	fm := &fakeModem{}
	r := testRotator(cfg, fm)
	call := 0
	r.CurrentIPIs = func() (string, error) {
		call++
		if call <= 1 {
			return "", nil
		}
		return "9.9.9.9", nil
	}

	oldIP, newIP, err := r.RotateReboot()
	if err != nil {
		t.Fatalf("RotateReboot() error = %v", err)
	}
	if oldIP != "" {
		t.Errorf("old IP = %q, want empty", oldIP)
	}
	if newIP != "9.9.9.9" {
		t.Errorf("new IP = %q, want %q", newIP, "9.9.9.9")
	}
	if !fm.rebootCalled {
		t.Error("expected Reboot to be called (deep clean)")
	}
	if fm.setMobileDataCalled {
		t.Error("SetMobileData should NOT be called for hardware reboot")
	}
}

func TestRotateRebootFailsOnModemError(t *testing.T) {
	cfg := testConfig(map[string]any{
		"connect_attempts": 3,
	})

	r := testRotator(cfg, &fakeModem{rebootErr: fmt.Errorf("reboot failed")})
	r.CurrentIPIs = func() (string, error) { return "1.1.1.1", nil }

	_, _, err := r.RotateReboot()
	if err == nil {
		t.Fatal("expected error from reboot failure")
	}
}

type fakeModem struct {
	rebootCalled        bool
	setMobileDataCalled bool
	rebootErr           error
	setMobileDataErr    error
}

func (f *fakeModem) Reboot() error {
	f.rebootCalled = true
	return f.rebootErr
}

func (f *fakeModem) SetMobileData(enabled bool) error {
	f.setMobileDataCalled = true
	return f.setMobileDataErr
}
