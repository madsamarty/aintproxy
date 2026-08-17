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

// Drivers returns all registered drivers.
func Drivers() []*Driver {
	return drivers
}
