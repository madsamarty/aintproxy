package devices

import (
	"fmt"
	"os/exec"
	"strings"
)

type Device struct {
	Interface string
	Type      string
	State     string
	Connection string
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

// ListModems returns only Huawei modem interfaces (usb, wwan types).
func ListModems(runCmd func(string, ...string) ([]byte, error)) ([]Device, error) {
	all, err := ListAll(runCmd)
	if err != nil {
		return nil, err
	}
	var modems []Device
	for _, d := range all {
		if isModemType(d.Type) || isHuaweiInterface(d.Interface) {
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
	return t == "usb" || t == "wwan"
}

func isHuaweiInterface(name string) bool {
	name = strings.ToLower(name)
	prefixes := []string{"vodafone", "huawei", "wwan", "usb"}
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
