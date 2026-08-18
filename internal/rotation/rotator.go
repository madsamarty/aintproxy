package rotation

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mohamed-sameh/aintproxy/internal/config"
	"github.com/mohamed-sameh/aintproxy/internal/modem"
)

type RotationError struct {
	Msg string
}

func (e *RotationError) Error() string {
	return e.Msg
}

const lastRotationFile = "/var/lib/aintproxy/.last_rotation"

type Rotator struct {
	Config      *config.Config
	Modem       modem.Modem
	Logger      *slog.Logger
	DryRun      bool
	RunCmd      func(name string, args ...string) ([]byte, error)
	CurrentIPIs func() (string, error)
}

func New(cfg *config.Config, logger *slog.Logger) (*Rotator, error) {
	m, driverName, err := modem.Detect(cfg.Modem.IP, cfg.Modem.User, cfg.Modem.Password)
	if err != nil {
		return nil, err
	}
	logger.Info("Detected modem", "driver", driverName)

	r := &Rotator{
		Config: cfg,
		Modem:  m,
		Logger: logger,
	}
	r.RunCmd = r.defaultRunCmd
	return r, nil
}

func (r *Rotator) defaultRunCmd(name string, args ...string) ([]byte, error) {
	if r.Config.Network.UseSudo && os.Geteuid() != 0 {
		allArgs := append([]string{name}, args...)
		cmd := exec.Command("sudo", allArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return out, fmt.Errorf("command failed (%s %v): %s", name, args, string(out))
		}
		return out, nil
	}
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("command failed (%s %v): %s", name, args, string(out))
	}
	return out, nil
}

func (r *Rotator) CurrentIP() (string, error) {
	if r.CurrentIPIs != nil {
		return r.CurrentIPIs()
	}

	proxyURL := fmt.Sprintf("http://%s:%d", r.Config.Network.ProxyHost, r.Config.Network.ProxyPort)
	proxy, _ := url.Parse(proxyURL)

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxy),
		},
	}

	// Try primary URL first, then fallbacks
	urls := r.ipCheckURLs()
	for _, checkURL := range urls {
		resp, err := client.Get(checkURL)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		ip := strings.TrimSpace(string(body))
		if ip != "" {
			return ip, nil
		}
	}
	return "", nil
}

// ipCheckURLs returns the primary IP check URL plus common fallbacks.
func (r *Rotator) ipCheckURLs() []string {
	primary := r.Config.Network.IPCheckURL
	fallbacks := []string{
		"https://api.ipify.org",
		"https://ifconfig.me",
		"https://icanhazip.com",
		"https://checkip.amazonaws.com",
	}

	seen := map[string]bool{primary: true}
	urls := []string{primary}
	for _, fb := range fallbacks {
		if !seen[fb] {
			urls = append(urls, fb)
			seen[fb] = true
		}
	}
	return urls
}

func (r *Rotator) RebootModem() error {
	r.Logger.Info("Rebooting modem...")
	return r.Modem.Reboot()
}

func (r *Rotator) ToggleMobileData() error {
	r.Logger.Info("Disabling mobile data...")
	if err := r.Modem.SetMobileData(false); err != nil {
		return err
	}
	r.Logger.Info("Waiting for ISP to release lease", "seconds", r.Config.Rotation.ToggleWait)
	time.Sleep(time.Duration(r.Config.Rotation.ToggleWait) * time.Second)
	r.Logger.Info("Enabling mobile data...")
	if err := r.Modem.SetMobileData(true); err != nil {
		r.Logger.Warn("Mobile data re-enable failed, retrying recovery...", "error", err)
		for i := 1; i <= 5; i++ {
			time.Sleep(15 * time.Second)
			r.Logger.Info("Recovery attempt", "attempt", i, "max", 5)
			if err := r.Modem.SetMobileData(true); err == nil {
				r.Logger.Info("Mobile data re-enabled successfully")
				return nil
			}
		}
		return fmt.Errorf("failed to re-enable mobile data after rotation: modem may need manual intervention")
	}
	return nil
}

func (r *Rotator) ReconnectInterface() error {
	r.Logger.Info("Reconnecting interface", "interface", r.Config.Modem.Interface)
	_, err := r.RunCmd("nmcli", "device", "connect", r.Config.Modem.Interface)
	if err != nil {
		return &RotationError{Msg: fmt.Sprintf("reconnect interface: %v", err)}
	}

	for attempt := 1; attempt <= r.Config.Rotation.IPCheckAttempts; attempt++ {
		if r.interfaceHasIP() {
			r.Logger.Info("Interface has IP", "interface", r.Config.Modem.Interface)
			r.setHighMetric()
			return nil
		}
		r.Logger.Info("Waiting for interface IP...", "attempt", attempt, "max", r.Config.Rotation.IPCheckAttempts)
		time.Sleep(time.Duration(r.Config.Rotation.IPCheckInterval) * time.Second)
	}

	return &RotationError{Msg: fmt.Sprintf("interface %s has no IP after reconnect", r.Config.Modem.Interface)}
}

func (r *Rotator) interfaceHasIP() bool {
	out, err := r.RunCmd("ip", "-4", "addr", "show", "dev", r.Config.Modem.Interface)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "inet ")
}

func (r *Rotator) setHighMetric() {
	r.Logger.Info("Setting high metric on interface to avoid stealing default route",
		"interface", r.Config.Modem.Interface, "metric", r.Config.Network.RouteMetric)
	r.RunCmd("ip", "route", "replace", "default",
		"via", r.Config.Modem.IP,
		"dev", r.Config.Modem.Interface,
		"metric", fmt.Sprintf("%d", r.Config.Network.RouteMetric))
}

func (r *Rotator) ApplyRouting() error {
	if !r.interfaceHasIP() {
		r.Logger.Warn("Skipping routing: interface has no IP", "interface", r.Config.Modem.Interface)
		return &RotationError{Msg: fmt.Sprintf("interface %s has no IP, skipping routing to protect network", r.Config.Modem.Interface)}
	}

	r.Logger.Info("Applying routing rules")

	r.RunCmd("sysctl", "-w",
		fmt.Sprintf("net.ipv4.conf.%s.rp_filter=2", r.Config.Modem.Interface))

	r.RunCmd("ip", "route", "replace", "default",
		"via", r.Config.Modem.IP,
		"dev", r.Config.Modem.Interface,
		"table", r.Config.Network.RoutingTable)

	if r.Config.Network.LocalIP != "" {
		r.RunCmd("ip", "rule", "del",
			"from", r.Config.Network.LocalIP,
			"lookup", r.Config.Network.RoutingTable)

		r.RunCmd("ip", "rule", "add",
			"from", r.Config.Network.LocalIP,
			"lookup", r.Config.Network.RoutingTable)
	}

	return nil
}

func (r *Rotator) WaitForIP() (string, error) {
	for attempt := 1; attempt <= r.Config.Rotation.IPCheckAttempts; attempt++ {
		ip, _ := r.CurrentIP()
		if ip != "" {
			return ip, nil
		}
		r.Logger.Info("Still connecting...",
			"attempt", attempt,
			"max", r.Config.Rotation.IPCheckAttempts)
		time.Sleep(time.Duration(r.Config.Rotation.IPCheckInterval) * time.Second)
	}
	return "", &RotationError{Msg: "timed out waiting for a new public IP"}
}

func (r *Rotator) checkCooldown() error {
	if r.Config.Rotation.Cooldown <= 0 {
		return nil
	}
	data, err := os.ReadFile(lastRotationFile)
	if err != nil {
		return nil
	}
	var last time.Time
	if err := last.UnmarshalText(data); err != nil {
		return nil
	}
	remaining := time.Duration(r.Config.Rotation.Cooldown)*time.Second - time.Since(last)
	if remaining > 0 {
		return &RotationError{Msg: fmt.Sprintf("cooldown active, try again in %s", remaining.Round(time.Second))}
	}
	return nil
}

func (r *Rotator) recordRotation() {
	os.MkdirAll("/var/lib/aintproxy", 0755)
	ts, _ := time.Now().Truncate(time.Microsecond).MarshalText()
	os.WriteFile(lastRotationFile, ts, 0644)
}

func (r *Rotator) OnInterrupt() {
	r.Logger.Warn("Interrupted — re-enabling mobile data to avoid modem hang...")
	type singleShot interface {
		SetMobileDataOnce(enabled bool) error
	}
	if ss, ok := r.Modem.(singleShot); ok {
		if err := ss.SetMobileDataOnce(true); err != nil {
			r.Logger.Warn("Interrupt re-enable failed (non-critical), modem may need manual reset", "error", err)
		} else {
			r.Logger.Info("Mobile data re-enabled successfully")
		}
	} else {
		if err := r.Modem.SetMobileData(true); err != nil {
			r.Logger.Warn("Interrupt re-enable failed (non-critical), modem may need manual reset", "error", err)
		} else {
			r.Logger.Info("Mobile data re-enabled successfully")
		}
	}
	r.recordRotation()
	os.Exit(1)
}

func (r *Rotator) WatchSignals() func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			r.OnInterrupt()
		case <-done:
		}
	}()
	return func() { close(done); signal.Stop(ch) }
}

// Rotate performs a fast software-level IP lease drop by toggling mobile data off/on.
func (r *Rotator) Rotate() (string, string, error) {
	if err := r.checkCooldown(); err != nil {
		return "", "", err
	}

	oldIP, _ := r.CurrentIP()
	r.Logger.Info("Current IP", "ip", oldIP)

	if r.DryRun {
		r.Logger.Info("Dry run — skipping mobile data toggle")
		newIP, _ := r.CurrentIP()
		return oldIP, orDefault(newIP, oldIP), nil
	}

	r.Logger.Info("Toggling mobile data (fast lease drop)")
	if err := r.ToggleMobileData(); err != nil {
		return "", "", err
	}

	if err := r.ReconnectInterface(); err != nil {
		return oldIP, "", err
	}
	if err := r.ApplyRouting(); err != nil {
		return oldIP, "", err
	}

	newIP, err := r.WaitForIP()
	if err != nil {
		return oldIP, "", err
	}
	r.recordRotation()
	r.Logger.Info("New IP", "ip", newIP)
	return oldIP, newIP, nil
}

// RotateReboot performs a full hardware modem reboot to force a new IP lease.
func (r *Rotator) RotateReboot() (string, string, error) {
	if err := r.checkCooldown(); err != nil {
		return "", "", err
	}

	oldIP, _ := r.CurrentIP()
	r.Logger.Info("Current IP", "ip", oldIP)

	if r.DryRun {
		r.Logger.Info("Dry run — skipping modem reboot")
		newIP, _ := r.CurrentIP()
		return oldIP, orDefault(newIP, oldIP), nil
	}

	r.Logger.Info("Rebooting modem (deep clean)")
	if err := r.RebootModem(); err != nil {
		return "", "", err
	}
	r.Logger.Info("Modem restarting...",
		"seconds", r.Config.Rotation.RebootWait)
	time.Sleep(time.Duration(r.Config.Rotation.RebootWait) * time.Second)

	if err := r.ReconnectInterface(); err != nil {
		return oldIP, "", err
	}
	if err := r.ApplyRouting(); err != nil {
		return oldIP, "", err
	}

	newIP, err := r.WaitForIP()
	if err != nil {
		return oldIP, "", err
	}
	r.recordRotation()
	r.Logger.Info("New IP", "ip", newIP)
	return oldIP, newIP, nil
}

type RotateResult struct {
	Interface string `json:"interface"`
	OldIP     string `json:"old_ip"`
	NewIP     string `json:"new_ip"`
	Err       error  `json:"-"`
}

func (r RotateResult) MarshalJSON() ([]byte, error) {
	type Alias RotateResult
	errStr := ""
	if r.Err != nil {
		errStr = r.Err.Error()
	}
	return json.Marshal(struct {
		Error string `json:"error,omitempty"`
		Alias
	}{
		Error: errStr,
		Alias: (Alias)(r),
	})
}

// HardRotate performs a full hardware modem reboot on the configured interface.
func (r *Rotator) HardRotate() ([]RotateResult, error) {
	oldIP, newIP, err := r.RotateReboot()
	return []RotateResult{{
		Interface: r.Config.Modem.Interface,
		OldIP:     oldIP,
		NewIP:     newIP,
		Err:       err,
	}}, nil
}

type InfoResult struct {
	PublicIP    string `json:"public_ip"`
	Interface   string `json:"interface"`
	ModemIP     string `json:"modem_ip"`
	ModemState  string `json:"modem_state"`
	InterfaceOK bool   `json:"interface_ok"`
}

// Info returns current rotation status for the configured interface.
func (r *Rotator) Info() (*InfoResult, error) {
	info := &InfoResult{
		Interface: r.Config.Modem.Interface,
		ModemIP:   r.Config.Modem.IP,
	}

	ip, _ := r.CurrentIP()
	info.PublicIP = ip

	out, err := r.RunCmd("nmcli", "-t", "-f", "DEVICE,STATE", "device", "status")
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && parts[0] == r.Config.Modem.Interface {
				info.ModemState = parts[1]
				info.InterfaceOK = parts[1] == "connected"
				break
			}
		}
	}

	return info, nil
}

func orDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
