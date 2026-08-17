package config

import (
	"fmt"
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
}

type RotationConfig struct {
	RebootWait      int `yaml:"reboot_wait"`
	ToggleWait      int `yaml:"data_off_wait"`
	IPCheckAttempts int `yaml:"ip_check_attempts"`
	IPCheckInterval int `yaml:"ip_check_interval"`
}

type ServerConfig struct {
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token"`
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
		},
		Rotation: RotationConfig{
			RebootWait:      35,
			ToggleWait:      35,
			IPCheckAttempts: 15,
			IPCheckInterval: 3,
		},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 5000,
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

	return nil
}
