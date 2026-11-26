// Package config defines the service configuration loaded by the butterfly
// core framework from config.yaml.
package config

import (
	"errors"
	"log/slog"

	"gopkg.in/yaml.v3"
)

// ServiceConfig is the root configuration for clash-gen.
type ServiceConfig struct {
	Environment string            `yaml:"environment"`
	BaseURL     string            `yaml:"base_url"`
	AdminToken  string            `yaml:"admin_token"`
	RedisURL    string            `yaml:"redis_url"`
	ObjectStore ObjectStoreConfig `yaml:"object_store"`
}

type ObjectStoreConfig struct {
	Endpoint        string `yaml:"endpoint"`
	CDNDomain       string `yaml:"cdn_domain"`
	Bucket          string `yaml:"bucket"`
	Region          string `yaml:"region"`
	Prefix          string `yaml:"prefix"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	PathStyle       *bool  `yaml:"path_style"`
}

func (c *ServiceConfig) UnmarshalYAML(node *yaml.Node) error {
	type serviceConfig ServiceConfig
	if err := node.Decode((*serviceConfig)(c)); err != nil {
		return errors.New("invalid clash-gen configuration; check field names and value types")
	}
	return nil
}

// Print logs the loaded configuration; required by the butterfly AppConfig interface.
func (c *ServiceConfig) Print() {
	slog.Info("service config loaded", "environment", c.Environment)
}
