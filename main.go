package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mohamed-sameh/aintscrape/internal/config"
	"github.com/mohamed-sameh/aintscrape/internal/devices"
	"github.com/mohamed-sameh/aintscrape/internal/modem"
	"github.com/mohamed-sameh/aintscrape/internal/rotation"
	"github.com/mohamed-sameh/aintscrape/internal/scrape"
	"github.com/mohamed-sameh/aintscrape/internal/server"

	_ "github.com/mohamed-sameh/aintscrape/internal/huawei"
	_ "github.com/mohamed-sameh/aintscrape/internal/tplink"
	_ "github.com/mohamed-sameh/aintscrape/internal/zte"
)

//go:embed config.example.yaml
var defaultConfig []byte

const version = "0.2.0"

func main() {
	configPath := flag.String("config", config.DefaultConfigPath, "path to config file")
	jsonOutput := flag.Bool("json", false, "output in JSON format")
	dryRun := flag.Bool("dry-run", false, "simulate rotation without toggling data")
	flag.Usage = printUsage
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	cmd := flag.Arg(0)

	switch cmd {
	case "version":
		fmt.Printf("aintscrape %s\n", version)
		os.Exit(0)

	case "help", "--help", "-h":
		printUsage()
		os.Exit(0)

	case "devices":
		runDevices(*jsonOutput)
		return

	case "drivers":
		runDrivers(*jsonOutput)
		return

	case "config":
		runConfig(*configPath)
		return

	case "scrape":
		runScrape(flag.Args()[1:], *configPath, *jsonOutput)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(2)
	}

	logger := setupLogger(cfg.Server.LogLevel)

	rot, err := rotation.New(cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Modem error: %v\n", err)
		os.Exit(2)
	}

	rot.DryRun = *dryRun

	switch cmd {
	case "fix":
		runFix(cfg, logger, *jsonOutput)
		return

	case "rotate":
		stop := rot.WatchSignals()
		oldIP, newIP, err := rot.Rotate()
		stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *jsonOutput {
			json.NewEncoder(os.Stdout).Encode(map[string]any{
				"old_ip":  orUnknown(oldIP),
				"new_ip":  orUnknown(newIP),
				"rotated": oldIP != newIP,
				"dry_run": *dryRun,
			})
		} else {
			fmt.Printf("Old IP: %s\n", orUnknown(oldIP))
			fmt.Printf("New IP: %s\n", orUnknown(newIP))
			if *dryRun {
				fmt.Println("Dry run — no data was toggled")
			}
		}

	case "hard-rotate":
		stop := rot.WatchSignals()
		results, err := rot.HardRotate()
		stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *jsonOutput {
			json.NewEncoder(os.Stdout).Encode(map[string]any{
				"results": results,
				"dry_run": *dryRun,
			})
		} else {
			for _, r := range results {
				if r.Err != nil {
					fmt.Fprintf(os.Stderr, "[%s] Error: %v\n", r.Interface, r.Err)
				} else {
					fmt.Printf("[%s] Old IP: %s  New IP: %s\n", r.Interface, orUnknown(r.OldIP), r.NewIP)
				}
			}
			if *dryRun {
				fmt.Println("Dry run — no data was toggled")
			}
		}

	case "info":
		info, err := rot.Info()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *jsonOutput {
			json.NewEncoder(os.Stdout).Encode(info)
		} else {
			fmt.Printf("Public IP:    %s\n", orUnknown(info.PublicIP))
			fmt.Printf("Interface:    %s\n", info.Interface)
			fmt.Printf("Modem IP:     %s\n", info.ModemIP)
			fmt.Printf("Modem state:  %s\n", orUnknown(info.ModemState))
			if info.InterfaceOK {
				fmt.Printf("Status:       connected\n")
			} else {
				fmt.Printf("Status:       disconnected\n")
			}
		}

	case "serve":
		var driverName string
		_, detectedName, err := modem.Detect(cfg.Modem.IP, cfg.Modem.User, cfg.Modem.Password)
		if err == nil {
			driverName = detectedName
		}
		srv, err := server.NewWithDriver(cfg, logger, driverName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(2)
		}

		ctx, stop := signal.NotifyContext(context.Background(),
			syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		go func() {
			<-ctx.Done()
			logger.Info("Shutting down...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv.Shutdown(shutdownCtx)
		}()

		if err := srv.Start(); err != nil && err != context.Canceled {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}

func setupLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func runDevices(jsonOutput bool) {
	runCmd := func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}

	devs, err := devices.ListAll(runCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(devs) == 0 {
		if jsonOutput {
			json.NewEncoder(os.Stdout).Encode([]devices.Device{})
		} else {
			fmt.Println("No network devices found.")
		}
		return
	}

	devices.EnrichWithModemInfo(devs, runCmd)

	if jsonOutput {
		json.NewEncoder(os.Stdout).Encode(devs)
	} else {
		fmt.Printf("%-15s %-10s %-15s %-20s %s\n", "DEVICE", "TYPE", "STATE", "MODEM MODEL", "CONNECTION")
		fmt.Printf("%-15s %-10s %-15s %-20s %s\n", "------", "----", "-----", "-----------", "----------")
		for _, d := range devs {
			model := d.ModemModel
			if model == "" {
				model = "-"
			}
			fmt.Printf("%-15s %-10s %-15s %-20s %s\n", d.Interface, d.Type, d.State, model, d.Connection)
		}
	}
}

func runDrivers(jsonOutput bool) {
	drivers := modem.Drivers()
	if len(drivers) == 0 {
		if jsonOutput {
			json.NewEncoder(os.Stdout).Encode([]modem.Driver{})
		} else {
			fmt.Println("No modem drivers registered.")
		}
		return
	}

	type driverInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}

	var supported, planned []driverInfo
	for _, d := range drivers {
		info := driverInfo{Name: d.Name, Description: d.Description, Status: d.Status}
		switch d.Status {
		case "supported":
			supported = append(supported, info)
		default:
			planned = append(planned, info)
		}
	}

	if jsonOutput {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"supported": supported,
			"planned":   planned,
		})
	} else {
		if len(supported) > 0 {
			fmt.Println("SUPPORTED DRIVERS")
			for _, d := range supported {
				fmt.Printf("  %-20s %s\n", d.Name, d.Description)
			}
		}
		if len(planned) > 0 {
			fmt.Println("\nPLANNED")
			for _, d := range planned {
				fmt.Printf("  %-20s %s\n", d.Name, d.Description)
			}
		}
	}
}

func runConfig(path string) {
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "Config already exists at %s\n", path)
		os.Exit(1)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(path, defaultConfig, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config written to %s\n", path)
	fmt.Println("Edit it with your modem password, then run: aintscrape rotate")
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "aintscrape %s — web scraper and modem IP rotator\n\n", version)
	fmt.Fprintf(os.Stderr, "Usage: aintscrape [--config <path>] [--json] [--dry-run] [--help] <command>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  config       Install default config to /etc/aintscrape/config.yaml\n")
	fmt.Fprintf(os.Stderr, "  scrape       Fetch a URL and extract data (see: scrape -h)\n")
	fmt.Fprintf(os.Stderr, "  rotate       Fast software-level IP lease drop (toggle mobile data)\n")
	fmt.Fprintf(os.Stderr, "  hard-rotate  Full hardware modem reboot/reset on all interfaces\n")
	fmt.Fprintf(os.Stderr, "  fix          Force re-enable mobile data and reconnect interface\n")
	fmt.Fprintf(os.Stderr, "  devices      List all network devices and their status\n")
	fmt.Fprintf(os.Stderr, "  drivers      List supported modem drivers\n")
	fmt.Fprintf(os.Stderr, "  info         Show current IP, interface, and modem status\n")
	fmt.Fprintf(os.Stderr, "  serve        Start the HTTP rotation service\n")
	fmt.Fprintf(os.Stderr, "  help         Show this help message\n")
	fmt.Fprintf(os.Stderr, "  version      Print version and exit\n")
	fmt.Fprintf(os.Stderr, "\nFlags:\n")
	fmt.Fprintf(os.Stderr, "  --config <path>   path to config file (default %q)\n", config.DefaultConfigPath)
	fmt.Fprintf(os.Stderr, "  --json            output in JSON format\n")
	fmt.Fprintf(os.Stderr, "  --dry-run         simulate rotation without toggling data\n")
	fmt.Fprintf(os.Stderr, "  --help            show this help message\n")
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func runFix(cfg *config.Config, logger *slog.Logger, jsonOutput bool) {
	type singleShot interface {
		SetMobileDataOnce(enabled bool) error
	}

	m, driverName, err := modem.Detect(cfg.Modem.IP, cfg.Modem.User, cfg.Modem.Password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Modem error: %v\n", err)
		os.Exit(1)
	}
	logger.Info("Detected modem", "driver", driverName)

	out, _ := exec.Command("sudo", "ip", "-4", "addr", "show", "dev", cfg.Modem.Interface).CombinedOutput()
	hasIP := strings.Contains(string(out), "inet ")

	if hasIP {
		fmt.Printf("Interface %s already has an IP — skipping.\n", cfg.Modem.Interface)
		return
	}

	fmt.Println("Interface has no IP — enabling mobile data...")
	var enableErr error
	if ss, ok := m.(singleShot); ok {
		enableErr = ss.SetMobileDataOnce(true)
	} else {
		enableErr = m.SetMobileData(true)
	}
	if enableErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to enable mobile data: %v\n", enableErr)
		os.Exit(1)
	}
	fmt.Println("Mobile data enabled.")

	fmt.Println("Reconnecting interface...")
	out, err = exec.Command("sudo", "nmcli", "device", "connect", cfg.Modem.Interface).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Reconnect failed: %s\n", string(out))
		os.Exit(1)
	}

	fmt.Println("Waiting for IP...")
	time.Sleep(5 * time.Second)
	out, _ = exec.Command("sudo", "ip", "-4", "addr", "show", "dev", cfg.Modem.Interface).CombinedOutput()
	if strings.Contains(string(out), "inet ") {
		fmt.Println("Interface is up with IP.")
	} else {
		fmt.Println("Interface connected but no IP yet — may need a moment.")
	}
}

func runScrape(args []string, configPath string, jsonOutput bool) {
	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: aintscrape [--json] scrape [flags] <url>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	useProxy := fs.Bool("proxy", false, "route the request through the local aintscrape proxy (from config)")
	proxyURL := fs.String("proxy-url", "", "route through this proxy URL instead of the configured one")
	selector := fs.String("select", "", "CSS selector; print each match (default: print raw body)")
	attr := fs.String("attr", "", "with --select: print this attribute instead of text")
	rawHTML := fs.Bool("html", false, "with --select: print inner HTML instead of text")
	ua := fs.String("user-agent", "", "User-Agent header")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	retries := fs.Int("retries", 2, "retries on network error, 429 or 5xx")

	// Allow the URL to appear before or after flags.
	var target string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			os.Exit(2)
		}
		rest = fs.Args()
		if len(rest) > 0 {
			if target != "" {
				fmt.Fprintln(os.Stderr, "scrape: only one URL is supported")
				os.Exit(2)
			}
			target, rest = rest[0], rest[1:]
		}
	}
	if target == "" {
		fs.Usage()
		os.Exit(2)
	}

	opts := scrape.Options{UserAgent: *ua, Timeout: *timeout, Retries: *retries, ProxyURL: *proxyURL}
	if *useProxy && opts.ProxyURL == "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
			os.Exit(2)
		}
		opts.ProxyURL = fmt.Sprintf("http://%s:%d", cfg.Network.ProxyHost, cfg.Network.ProxyPort)
	}

	res, err := scrape.Fetch(target, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fetch error: %v\n", err)
		os.Exit(1)
	}

	var items []string
	if *selector != "" {
		items, err = scrape.Select(res.Body, scrape.Extract{Selector: *selector, Attr: *attr, HTML: *rawHTML})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if jsonOutput {
		out := map[string]any{"url": res.URL, "status": res.Status, "proxy": opts.ProxyURL}
		if *selector != "" {
			out["matches"] = items
		} else {
			out["body"] = string(res.Body)
		}
		json.NewEncoder(os.Stdout).Encode(out)
	} else if *selector != "" {
		for _, it := range items {
			fmt.Println(it)
		}
	} else {
		os.Stdout.Write(res.Body)
	}

	if res.Status >= 400 {
		fmt.Fprintf(os.Stderr, "HTTP %d\n", res.Status)
		os.Exit(1)
	}
}
