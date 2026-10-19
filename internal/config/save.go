package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ifaceLine = regexp.MustCompile(`^(\s+interface:\s*)("[^"]*"|\S*)(.*)$`)
	ipLine    = regexp.MustCompile(`^(\s+ip:\s*)("[^"]*"|\S*)(.*)$`)
)

// SetModemDevice rewrites modem.interface (and modem.ip when non-empty) in the
// config file at path, leaving comments and all other settings untouched.
func SetModemDevice(path, iface, ip string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inModem, setIface := false, false
	for i, l := range lines {
		if len(l) > 0 && l[0] != ' ' && l[0] != '\t' && l[0] != '#' {
			inModem = strings.HasPrefix(l, "modem:")
			continue
		}
		if !inModem {
			continue
		}
		if m := ifaceLine.FindStringSubmatch(l); m != nil {
			lines[i] = fmt.Sprintf("%s%q%s", m[1], iface, m[3])
			setIface = true
		} else if m := ipLine.FindStringSubmatch(l); m != nil && ip != "" {
			lines[i] = fmt.Sprintf("%s%q%s", m[1], ip, m[3])
		}
	}
	if !setIface {
		return fmt.Errorf("modem.interface not found in %s", path)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600)
}

// PeekModem reads just the modem section without validating the rest of the
// config, for display purposes.
func PeekModem(path string) (ModemConfig, error) {
	cfg := defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		return ModemConfig{}, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return ModemConfig{}, err
	}
	return cfg.Modem, nil
}

// DefaultOutputDir is ~/aintscrape-output for the invoking user (the real user
// even when running under sudo).
func DefaultOutputDir() string {
	home := ""
	if su := os.Getenv("SUDO_USER"); su != "" {
		if u, err := user.Lookup(su); err == nil {
			home = u.HomeDir
		}
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, "aintscrape-output")
}

// ResolveOutputDir expands ~ and applies the default for an empty setting.
func ResolveOutputDir(dir string) string {
	if dir == "" {
		return DefaultOutputDir()
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		return filepath.Join(filepath.Dir(DefaultOutputDir()), strings.TrimPrefix(dir, "~"))
	}
	return dir
}

// PeekOutputDir returns the resolved output directory from the config file,
// falling back to the default when unset or unreadable.
func PeekOutputDir(path string) string {
	cfg := defaults()
	if data, err := os.ReadFile(path); err == nil {
		yaml.Unmarshal(data, cfg)
	}
	return ResolveOutputDir(cfg.Output.Dir)
}

var outDirLine = regexp.MustCompile(`^(\s+dir:\s*)("[^"]*"|\S*)(.*)$`)

// SetOutputDir writes output.dir, adding the section if the file lacks it.
func SetOutputDir(path, dir string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inOut, done := false, false
	for i, l := range lines {
		if len(l) > 0 && l[0] != ' ' && l[0] != '\t' && l[0] != '#' {
			inOut = strings.HasPrefix(l, "output:")
			continue
		}
		if inOut {
			if m := outDirLine.FindStringSubmatch(l); m != nil {
				lines[i] = fmt.Sprintf("%s%q%s", m[1], dir, m[3])
				done = true
			}
		}
	}
	out := strings.Join(lines, "\n")
	if !done {
		out = strings.TrimRight(out, "\n") + fmt.Sprintf("\n\noutput:\n  dir: %q\n", dir)
	}
	return os.WriteFile(path, []byte(out), 0600)
}
