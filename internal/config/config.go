package config

import (
	"fmt"
	"net"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/aintproxy/config.yaml"

type Config struct {
	Modem    ModemConfig    `yaml:"modem"`
	Network  NetworkConfig  `yaml:"network"`
	Rotation RotationConfig `yaml:"rotation"`
	Server   ServerConfig   `yaml:"server"`
}

type ModemConfig struct {
	IP        string `yaml:"ip"`
	User      string `yaml:"user"`
	Password  string `yaml:"password"`
	Interface string `yaml:"interface"`
}

type NetworkConfig struct {
	LocalIP        string `yaml:"local_ip"`
	RoutingTable   string `yaml:"routing_table"`
	ProxyHost      string `yaml:"proxy_host"`
	ProxyPort      int    `yaml:"proxy_port"`
	IPCheckURL     string `yaml:"ip_check_url"`
	UseSudo        bool   `yaml:"use_sudo"`
	RouteMetric    int    `yaml:"route_metric"`
}

type RotationConfig struct {
	RebootWait      int `yaml:"reboot_wait"`
	ToggleWait      int `yaml:"data_off_wait"`
	IPCheckAttempts int `yaml:"ip_check_attempts"`
	IPCheckInterval int `yaml:"ip_check_interval"`
	Cooldown        int `yaml:"cooldown"`
}

type ServerConfig struct {
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token"`
	LogLevel  string `yaml:"log_level"`
}

func defaults() *Config {
	return &Config{
		Modem: ModemConfig{
			IP:        "192.168.8.1",
			User:      "admin",
			Password:  "",
			Interface: "vodafone0",
		},
		Network: NetworkConfig{
			LocalIP:        "",
			RoutingTable:   "100",
			ProxyHost:      "127.0.0.1",
			ProxyPort:      3128,
			IPCheckURL:     "https://api.ipify.org",
			UseSudo:        true,
			RouteMetric:    9000,
		},
		Rotation: RotationConfig{
			RebootWait:      35,
			ToggleWait:      35,
			IPCheckAttempts: 15,
			IPCheckInterval: 3,
			Cooldown:        120,
		},
		Server: ServerConfig{
			Host:     "0.0.0.0",
			Port:     5000,
			LogLevel: "info",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Modem.Password == "" {
		return fmt.Errorf("modem.password is required")
	}

	if c.Network.RoutingTable == "" {
		return fmt.Errorf("network.routing_table is required")
	}

	if c.Modem.IP != "" && net.ParseIP(c.Modem.IP) == nil {
		return fmt.Errorf("modem.ip: invalid IP address %q", c.Modem.IP)
	}

	if c.Network.ProxyPort < 1 || c.Network.ProxyPort > 65535 {
		return fmt.Errorf("network.proxy_port: must be between 1 and 65535, got %d", c.Network.ProxyPort)
	}

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port: must be between 1 and 65535, got %d", c.Server.Port)
	}

	if c.Rotation.RebootWait < 0 {
		return fmt.Errorf("rotation.reboot_wait: must be >= 0, got %d", c.Rotation.RebootWait)
	}

	if c.Rotation.ToggleWait < 0 {
		return fmt.Errorf("rotation.data_off_wait: must be >= 0, got %d", c.Rotation.ToggleWait)
	}

	if c.Rotation.IPCheckAttempts < 1 {
		return fmt.Errorf("rotation.ip_check_attempts: must be >= 1, got %d", c.Rotation.IPCheckAttempts)
	}

	if c.Rotation.IPCheckInterval < 0 {
		return fmt.Errorf("rotation.ip_check_interval: must be >= 0, got %d", c.Rotation.IPCheckInterval)
	}

	if c.Rotation.Cooldown < 0 {
		return fmt.Errorf("rotation.cooldown: must be >= 0, got %d", c.Rotation.Cooldown)
	}

	if c.Network.LocalIP != "" && net.ParseIP(c.Network.LocalIP) == nil {
		return fmt.Errorf("network.local_ip: invalid IP address %q", c.Network.LocalIP)
	}

	if c.Network.ProxyHost != "" && net.ParseIP(c.Network.ProxyHost) == nil {
		return fmt.Errorf("network.proxy_host: invalid IP address %q", c.Network.ProxyHost)
	}

	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if c.Server.LogLevel != "" && !validLogLevels[c.Server.LogLevel] {
		return fmt.Errorf("server.log_level: must be one of debug, info, warn, error — got %q", c.Server.LogLevel)
	}

	return nil
}
