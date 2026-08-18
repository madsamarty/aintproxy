package modem

import (
	"testing"
)

func TestRegisterAndDetect(t *testing.T) {
	// Save and restore global drivers
	orig := drivers
	defer func() { drivers = orig }()
	drivers = nil

	Register(&Driver{
		Name:        "test-supported",
		Description: "Test modem",
		Status:      "supported",
		Detect:      func(ip, user, pass string) bool { return ip == "192.168.1.1" },
		New:         func(ip, user, pass string) Modem { return &fakeModem{} },
	})

	m, name, err := Detect("192.168.1.1", "admin", "pass")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if name != "test-supported" {
		t.Errorf("driver name = %q, want test-supported", name)
	}
	if m == nil {
		t.Fatal("Detect() returned nil modem")
	}
}

func TestDetectNoMatch(t *testing.T) {
	orig := drivers
	defer func() { drivers = orig }()
	drivers = nil

	Register(&Driver{
		Name:        "test-supported",
		Description: "Test modem",
		Status:      "supported",
		Detect:      func(ip, user, pass string) bool { return false },
		New:         func(ip, user, pass string) Modem { return &fakeModem{} },
	})

	_, _, err := Detect("192.168.1.1", "admin", "pass")
	if err == nil {
		t.Error("expected error when no driver matches")
	}
}

func TestDetectSkipsPlanned(t *testing.T) {
	orig := drivers
	defer func() { drivers = orig }()
	drivers = nil

	Register(&Driver{
		Name:        "test-planned",
		Description: "Test modem",
		Status:      "planned",
		Detect:      func(ip, user, pass string) bool { return true },
		New:         func(ip, user, pass string) Modem { return &fakeModem{} },
	})

	_, _, err := Detect("192.168.1.1", "admin", "pass")
	if err == nil {
		t.Error("expected error when only planned drivers exist")
	}
}

func TestDriversReturnsAll(t *testing.T) {
	orig := drivers
	defer func() { drivers = orig }()
	drivers = nil

	Register(&Driver{Name: "a", Status: "supported"})
	Register(&Driver{Name: "b", Status: "planned"})

	all := Drivers()
	if len(all) != 2 {
		t.Errorf("Drivers() returned %d drivers, want 2", len(all))
	}
}

func TestDetectModelNoMatch(t *testing.T) {
	orig := drivers
	defer func() { drivers = orig }()
	drivers = nil

	Register(&Driver{
		Name:        "test",
		Status:      "supported",
		DetectModel: func(ip string) (bool, string) { return false, "" },
	})

	model := DetectModel("192.168.1.1")
	if model != "" {
		t.Errorf("DetectModel() = %q, want empty", model)
	}
}

func TestDetectModelMatch(t *testing.T) {
	orig := drivers
	defer func() { drivers = orig }()
	drivers = nil

	Register(&Driver{
		Name:        "test",
		Status:      "supported",
		DetectModel: func(ip string) (bool, string) { return true, "TestModel" },
	})

	model := DetectModel("192.168.1.1")
	if model != "TestModel" {
		t.Errorf("DetectModel() = %q, want TestModel", model)
	}
}

type fakeModem struct{}

func (f *fakeModem) Reboot() error                       { return nil }
func (f *fakeModem) SetMobileData(enabled bool) error    { return nil }
