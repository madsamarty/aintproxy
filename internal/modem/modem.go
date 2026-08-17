package modem

import "fmt"

// Modem is the interface that all modem drivers must implement.
type Modem interface {
	Reboot() error
	SetMobileData(enabled bool) error
}

// Driver describes a modem driver and how to detect + create it.
type Driver struct {
	Name        string
	Description string
	Status      string // "supported", "experimental", "planned"
	Detect      func(ip, user, pass string) bool
	// DetectModel returns whether the modem is detected and its model name.
	// Used by the devices command to show modem info without full config.
	DetectModel func(ip string) (bool, string)
	New         func(ip, user, pass string) Modem
}

var drivers []*Driver

// Register adds a modem driver. Call from init() in each driver package.
// Drivers are tried in registration order during auto-detection.
func Register(d *Driver) {
	drivers = append(drivers, d)
}

// Detect iterates registered drivers and returns the first one that matches.
// Only drivers with a non-nil Detect function and "supported" status are tried.
func Detect(ip, user, pass string) (Modem, string, error) {
	for _, d := range drivers {
		if d.Detect == nil || d.Status != "supported" {
			continue
		}
		if d.Detect(ip, user, pass) {
			return d.New(ip, user, pass), d.Name, nil
		}
	}
	return nil, "", fmt.Errorf("could not detect modem at %s — check IP, credentials, and that your modem is supported", ip)
}

// DetectModel probes all registered drivers at the given IP and returns
// the model name of the first one that matches. Empty string if none matched.
func DetectModel(ip string) string {
	for _, d := range drivers {
		if d.DetectModel == nil || d.Status != "supported" {
			continue
		}
		if ok, model := d.DetectModel(ip); ok && model != "" {
			return model
		}
	}
	return ""
}

// Drivers returns all registered drivers.
func Drivers() []*Driver {
	return drivers
}
