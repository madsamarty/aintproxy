package devices

import (
	"fmt"
	"testing"
)

func mockRunCmd(output string, err error) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		return []byte(output), err
	}
}

func TestListAll(t *testing.T) {
	nmcli := `lo:loopback:connected:lo
eth0:ethernet:connected:Wired connection 1
vodafone0:usb:connected:vodafone
wwan0:wwan:disconnected:
docker0:bridge:connected:docker0`

	devs, err := ListAll(mockRunCmd(nmcli, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devs) != 5 {
		t.Fatalf("got %d devices, want 5", len(devs))
	}
	if devs[2].Interface != "vodafone0" {
		t.Errorf("device[2].Interface = %q, want %q", devs[2].Interface, "vodafone0")
	}
	if devs[2].Type != "usb" {
		t.Errorf("device[2].Type = %q, want %q", devs[2].Type, "usb")
	}
	if devs[2].State != "connected" {
		t.Errorf("device[2].State = %q, want %q", devs[2].State, "connected")
	}
}

func TestListModems(t *testing.T) {
	nmcli := `lo:loopback:connected:lo
eth0:ethernet:connected:Wired connection 1
vodafone0:usb:connected:vodafone
wwan0:wwan:disconnected:
docker0:bridge:connected:docker0`

	modems, err := ListModems(mockRunCmd(nmcli, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modems) != 2 {
		t.Fatalf("got %d modems, want 2", len(modems))
	}
	if modems[0].Interface != "vodafone0" {
		t.Errorf("modem[0] = %q, want %q", modems[0].Interface, "vodafone0")
	}
	if modems[1].Interface != "wwan0" {
		t.Errorf("modem[1] = %q, want %q", modems[1].Interface, "wwan0")
	}
}

func TestListModemsEmpty(t *testing.T) {
	nmcli := `lo:loopback:connected:lo
eth0:ethernet:connected:Wired connection 1`

	modems, err := ListModems(mockRunCmd(nmcli, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modems) != 0 {
		t.Errorf("got %d modems, want 0", len(modems))
	}
}

func TestListAllError(t *testing.T) {
	_, err := ListAll(mockRunCmd("", fmt.Errorf("nmcli not found")))
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestInterfaceInfo(t *testing.T) {
	nmcli := `lo:loopback:connected:lo
eth0:ethernet:connected:Wired connection 1
vodafone0:usb:connected:vodafone`

	info, err := InterfaceInfo("vodafone0", mockRunCmd(nmcli, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == "" {
		t.Error("expected non-empty info")
	}
}

func TestInterfaceInfoNotFound(t *testing.T) {
	nmcli := `lo:loopback:connected:lo`

	_, err := InterfaceInfo("vodafone0", mockRunCmd(nmcli, nil))
	if err == nil {
		t.Error("expected error for missing interface")
	}
}

func TestIsModemInterface(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"vodafone0", true},
		{"huawei0", true},
		{"wwan0", true},
		{"usb0", true},
		{"cdc-wdm0", true},
		{"eth0", false},
		{"lo", false},
		{"docker0", false},
	}
	for _, tc := range cases {
		if got := isModemInterface(tc.name); got != tc.want {
			t.Errorf("isModemInterface(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
