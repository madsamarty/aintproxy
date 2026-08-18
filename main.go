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
	"syscall"
	"time"

	"github.com/mohamed-sameh/aintproxy/internal/config"
	"github.com/mohamed-sameh/aintproxy/internal/devices"
	"github.com/mohamed-sameh/aintproxy/internal/modem"
	"github.com/mohamed-sameh/aintproxy/internal/rotation"
	"github.com/mohamed-sameh/aintproxy/internal/server"

	_ "github.com/mohamed-sameh/aintproxy/internal/huawei"
	_ "github.com/mohamed-sameh/aintproxy/internal/tplink"
	_ "github.com/mohamed-sameh/aintproxy/internal/zte"
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
		fmt.Printf("aintproxy %s\n", version)
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
	case "rotate":
		oldIP, newIP, err := rot.Rotate()
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
		results, err := rot.HardRotate()
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
	fmt.Println("Edit it with your modem password, then run: aintproxy rotate")
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "aintproxy %s — modem IP rotator\n\n", version)
	fmt.Fprintf(os.Stderr, "Usage: aintproxy [--config <path>] [--json] [--dry-run] [--help] <command>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  config       Install default config to /etc/aintproxy/config.yaml\n")
	fmt.Fprintf(os.Stderr, "  rotate       Fast software-level IP lease drop (toggle mobile data)\n")
	fmt.Fprintf(os.Stderr, "  hard-rotate  Full hardware modem reboot/reset on all interfaces\n")
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
