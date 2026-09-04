// Package config persists settings to ~/.config/mwb-client/config.json.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// Config mirrors the settings needed for both Client and Server roles.
// Client and server keys are independent (by design: key never goes on wire).
type Config struct {
	ClientHost     string                   `json:"clientHost"`     // IP or machine name to dial
	ClientKey      string                   `json:"clientKey"`      // security key for outbound
	ServerKey      string                   `json:"serverKey"`      // independent key for inbound
	Protocol       protocol.ProtocolVersion `json:"protocol"`       // auto|current|legacy (client)
	ServerProtocol protocol.ProtocolVersion `json:"serverProtocol"` // current|legacy (server, never auto)
	MachineName    string                   `json:"machineName"`
	ClipboardPort  int                      `json:"clipboardPort"`
	MessagePort    int                      `json:"messagePort"`
	AutoConnect    bool                     `json:"autoConnect"`
	ServerMode     bool                     `json:"serverMode"`

	KnownHosts map[string]string `json:"knownHosts"` // ip -> machine name
	Matrix     [4]string         `json:"matrix"`
	Wrap       bool              `json:"wrap"`
	TwoRow     bool              `json:"twoRow"`
}

// Defaults returns sane defaults (ports per protocol spec).
func Defaults() Config {
	return Config{
		Protocol:       protocol.ProtoAuto,
		ServerProtocol: protocol.ProtoCurrent,
		ClipboardPort:  protocol.ClipboardPort,
		MessagePort:    protocol.MessagePort,
		KnownHosts:     map[string]string{},
	}
}

// Path returns ~/.config/mwb-client/config.json (XDG aware).
func Path() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mwb-client", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mwb-client", "config.json")
}

// Load reads config or returns Defaults if missing.
func Load() (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.KnownHosts == nil {
		cfg.KnownHosts = map[string]string{}
	}
	if cfg.ClipboardPort == 0 {
		cfg.ClipboardPort = protocol.ClipboardPort
	}
	if cfg.MessagePort == 0 {
		cfg.MessagePort = protocol.MessagePort
	}
	if cfg.Protocol == "" {
		cfg.Protocol = protocol.ProtoAuto
	}
	if cfg.ServerProtocol == "" || cfg.ServerProtocol == protocol.ProtoAuto {
		cfg.ServerProtocol = protocol.ProtoCurrent
	}
	return cfg, nil
}

// Save writes config (creating parent dirs).
func Save(cfg Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// RecordKnownHost persists ip -> name mapping (LAN scan display).
func (c *Config) RecordKnownHost(ip, name string) {
	if c.KnownHosts == nil {
		c.KnownHosts = map[string]string{}
	}
	if name != "" {
		c.KnownHosts[ip] = name
	}
}
