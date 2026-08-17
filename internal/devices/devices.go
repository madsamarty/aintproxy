package devices

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mohamed-sameh/aintproxy/internal/modem"
)

type Device struct {
	Interface  string
	Type       string
	State      string
	Connection string
	Gateway    string
	ModemModel string
}

// ListAll returns all network devices from nmcli.
func ListAll(runCmd func(string, ...string) ([]byte, error)) ([]Device, error) {
	if runCmd == nil {
		runCmd = defaultRunCmd
	}
	out, err := runCmd("nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device", "status")
	if err != nil {
		return nil, fmt.Errorf("nmcli device status: %w", err)
	}
	return parseDevices(string(out)), nil
}

// ListModems returns only modem interfaces (usb, wwan, cdc-wdm, qmi, mbim types
// or interfaces with known modem name prefixes).
func ListModems(runCmd func(string, ...string) ([]byte, error)) ([]Device, error) {
	all, err := ListAll(runCmd)
	if err != nil {
		return nil, err
	}
	var modems []Device
	for _, d := range all {
		if isModemType(d.Type) || isModemInterface(d.Interface) {
			modems = append(modems, d)
		}
	}
	return modems, nil
}

// InterfaceInfo returns detailed info for a specific interface.
func InterfaceInfo(iface string, runCmd func(string, ...string) ([]byte, error)) (string, error) {
	if runCmd == nil {
		runCmd = defaultRunCmd
	}
	out, err := runCmd("nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device", "status")
	if err != nil {
		return "", fmt.Errorf("nmcli device status: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, ":", 4)
		if len(parts) >= 1 && parts[0] == iface {
			return formatDevice(parts), nil
		}
	}
	return "", fmt.Errorf("interface %s not found", iface)
}

func parseDevices(raw string) []Device {
	var devices []Device
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 4)
		d := Device{
			Interface: parts[0],
		}
		if len(parts) > 1 {
			d.Type = parts[1]
		}
		if len(parts) > 2 {
			d.State = parts[2]
		}
		if len(parts) > 3 {
			d.Connection = parts[3]
		}
		devices = append(devices, d)
	}
	return devices
}

func formatDevice(parts []string) string {
	iface := parts[0]
	typ := ""
	state := ""
	conn := ""
	if len(parts) > 1 {
		typ = parts[1]
	}
	if len(parts) > 2 {
		state = parts[2]
	}
	if len(parts) > 3 {
		conn = parts[3]
	}
	return fmt.Sprintf("Interface: %s  Type: %s  State: %s  Connection: %s", iface, typ, state, conn)
}

func isModemType(t string) bool {
	t = strings.ToLower(t)
	return t == "usb" || t == "wwan" || t == "cdc-wdm" || t == "qmi" || t == "mbim"
}

func isModemInterface(name string) bool {
	name = strings.ToLower(name)
	prefixes := []string{"vodafone", "huawei", "wwan", "usb", "cdc-wdm", "qmi", "mbim", "wwp", "usbmodem", "usb_"}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func defaultRunCmd(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// gatewayForIface returns the gateway IP for the given interface using ip route.
func gatewayForIface(iface string, runCmd func(string, ...string) ([]byte, error)) string {
	if runCmd == nil {
		runCmd = defaultRunCmd
	}
	out, err := runCmd("ip", "route", "show", "dev", iface)
	if err != nil {
		return ""
	}
	// Parse: "default via 192.168.8.1 dev vodafone0 proto dhcp ..."
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "default" && fields[1] == "via" {
			return fields[2]
		}
	}
	return ""
}

// probeModemModel delegates to registered modem drivers to detect the model.
func probeModemModel(gateway string) string {
	return modem.DetectModel(gateway)
}

// EnrichWithModemInfo probes interfaces for device model info.
// It checks all connected interfaces with a gateway, probing the gateway
// for known modem APIs (Huawei HiLink, ZTE, etc.).
func EnrichWithModemInfo(devs []Device, runCmd func(string, ...string) ([]byte, error)) {
	for i := range devs {
		d := &devs[i]
		if d.State != "connected" && !strings.HasPrefix(d.State, "connected") {
			continue
		}
		if d.Type == "loopback" || d.Type == "bridge" || d.Type == "tun" {
			continue
		}
		gw := gatewayForIface(d.Interface, runCmd)
		if gw == "" {
			continue
		}
		d.Gateway = gw
		d.ModemModel = probeModemModel(gw)
	}
}
