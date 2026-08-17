package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/mohamed-sameh/aintproxy/internal/config"
	"github.com/mohamed-sameh/aintproxy/internal/devices"
	"github.com/mohamed-sameh/aintproxy/internal/modem"
	"github.com/mohamed-sameh/aintproxy/internal/rotation"
	"github.com/mohamed-sameh/aintproxy/internal/server"

	_ "github.com/mohamed-sameh/aintproxy/internal/huawei"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	configPath := flag.String("config", config.DefaultConfigPath, "path to config file")
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
		runDevices()
		return

	case "drivers":
		runDrivers()
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(2)
	}

	rot, err := rotation.New(cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Modem error: %v\n", err)
		os.Exit(2)
	}

	switch cmd {
	case "rotate":
		oldIP, newIP, err := rot.Rotate()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Old IP: %s\n", orUnknown(oldIP))
		fmt.Printf("New IP: %s\n", newIP)

	case "hard-rotate":
		results, err := rot.HardRotate()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, r := range results {
			if r.Err != nil {
				fmt.Fprintf(os.Stderr, "[%s] Error: %v\n", r.Interface, r.Err)
			} else {
				fmt.Printf("[%s] Old IP: %s  New IP: %s\n", r.Interface, orUnknown(r.OldIP), r.NewIP)
			}
		}

	case "info":
		info, err := rot.Info()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Public IP:    %s\n", orUnknown(info.PublicIP))
		fmt.Printf("Interface:    %s\n", info.Interface)
		fmt.Printf("Modem IP:     %s\n", info.ModemIP)
		fmt.Printf("Modem state:  %s\n", orUnknown(info.ModemState))
		fmt.Printf("Rotation:     %s\n", info.Mode)
		if info.InterfaceOK {
			fmt.Printf("Status:       connected\n")
		} else {
			fmt.Printf("Status:       disconnected\n")
		}

	case "serve":
		srv, err := server.New(cfg, logger)
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
			srv.Shutdown(context.Background())
		}()

		if err := srv.Start(); err != nil && err != context.Canceled {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}

	case "reboot":
		if err := rot.RebootModem(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Reboot command sent to the modem.")

	case "toggle":
		if err := rot.ToggleMobileData(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Mobile data toggled.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}

func runDevices() {
	runCmd := func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}

	devs, err := devices.ListAll(runCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(devs) == 0 {
		fmt.Println("No network devices found.")
		return
	}

	fmt.Printf("%-15s %-10s %-15s %s\n", "DEVICE", "TYPE", "STATE", "CONNECTION")
	fmt.Printf("%-15s %-10s %-15s %s\n", "------", "----", "-----", "----------")
	for _, d := range devs {
		fmt.Printf("%-15s %-10s %-15s %s\n", d.Interface, d.Type, d.State, d.Connection)
	}
}

func runDrivers() {
	drivers := modem.Drivers()
	if len(drivers) == 0 {
		fmt.Println("No modem drivers registered.")
		return
	}

	var supported, planned []*modem.Driver
	for _, d := range drivers {
		switch d.Status {
		case "supported":
			supported = append(supported, d)
		default:
			planned = append(planned, d)
		}
	}

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

func printUsage() {
	fmt.Fprintf(os.Stderr, "aintproxy %s — modem IP rotator\n\n", version)
	fmt.Fprintf(os.Stderr, "Usage: aintproxy [--config path] <command>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  rotate       Fast software-level IP lease drop (toggle mobile data)\n")
	fmt.Fprintf(os.Stderr, "  hard-rotate  Full hardware modem reboot/reset on all interfaces\n")
	fmt.Fprintf(os.Stderr, "  devices      List all network devices and their status\n")
	fmt.Fprintf(os.Stderr, "  drivers      List supported modem drivers\n")
	fmt.Fprintf(os.Stderr, "  info         Show current IP, interface, and modem status\n")
	fmt.Fprintf(os.Stderr, "  serve        Start the HTTP rotation service\n")
	fmt.Fprintf(os.Stderr, "  reboot       Reboot the modem\n")
	fmt.Fprintf(os.Stderr, "  toggle       Toggle the modem mobile data off/on\n")
	fmt.Fprintf(os.Stderr, "  help         Show this help message\n")
	fmt.Fprintf(os.Stderr, "  version      Print version and exit\n")
	fmt.Fprintf(os.Stderr, "\nFlags:\n")
	flag.PrintDefaults()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
