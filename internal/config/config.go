// Package config defines the service configuration loaded by the butterfly
// core framework from config.yaml.
package config

import "log/slog"

// ServiceConfig is the root configuration for clash-gen.
type ServiceConfig struct {
	Environment string `yaml:"environment"`
	BaseURL     string `yaml:"base_url"`
	AdminToken  string `yaml:"admin_token"`
}

// Print logs the loaded configuration; required by the butterfly AppConfig interface.
func (c *ServiceConfig) Print() {
	slog.Info("service config loaded",
		"environment", c.Environment,
		"base_url", c.BaseURL,
	)
}
