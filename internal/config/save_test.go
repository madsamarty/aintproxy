package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetModemDevice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	src := "modem:\n  ip: \"192.168.1.1\"  # admin\n  interface: \"enp1s0\"  # nic\nserver:\n  port: 5000\n"
	os.WriteFile(p, []byte(src), 0600)
	if err := SetModemDevice(p, "vodafone0", "192.168.8.1"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	want := "modem:\n  ip: \"192.168.8.1\"  # admin\n  interface: \"vodafone0\"  # nic\nserver:\n  port: 5000\n"
	if string(got) != want {
		t.Fatalf("got:\n%s", got)
	}
	if err := SetModemDevice(p, "x", ""); err != nil || !strings.Contains(readF(p), `"x"`) {
		t.Fatal("empty ip case")
	}
}

func readF(p string) string { b, _ := os.ReadFile(p); return string(b) }
