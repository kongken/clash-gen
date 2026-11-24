package subscription

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// Settings keeps draft input for the editable Mihomo defaults.
type Settings struct {
	MixedPort    string `json:"mixed_port,omitempty"`
	LogLevel     string `json:"log_level,omitempty"`
	DNSEnabled   bool   `json:"dns_enabled,omitempty"`
	DNSUpstreams string `json:"dns_upstreams,omitempty"`
}

type mihomoSettings struct {
	MixedPort uint16    `yaml:"mixed-port"`
	LogLevel  string    `yaml:"log-level"`
	DNS       mihomoDNS `yaml:"dns"`
}

type mihomoDNS struct {
	Enable     bool     `yaml:"enable"`
	Nameserver []string `yaml:"nameserver,omitempty"`
}

func (s Settings) mihomo() (mihomoSettings, error) {
	settings := mihomoSettings{MixedPort: 7890, LogLevel: "info", DNS: mihomoDNS{Enable: s.DNSEnabled}}
	if value := strings.TrimSpace(s.MixedPort); value != "" {
		port, err := strconv.ParseUint(value, 10, 16)
		if err != nil || port == 0 {
			return mihomoSettings{}, errors.New("mixed-port 必须是 1 至 65535 的整数。")
		}
		settings.MixedPort = uint16(port)
	}
	switch s.LogLevel {
	case "":
	case "silent", "error", "warning", "info", "debug":
		settings.LogLevel = s.LogLevel
	default:
		return mihomoSettings{}, errors.New("请选择有效的日志级别。")
	}
	line := 0
	for raw := range strings.SplitSeq(s.DNSUpstreams, "\n") {
		line++
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		if !validDNSUpstream(address) {
			return mihomoSettings{}, fmt.Errorf("DNS 上游第 %d 行：请输入有效的地址。", line)
		}
		settings.DNS.Nameserver = append(settings.DNS.Nameserver, address)
	}
	if s.DNSEnabled && len(settings.DNS.Nameserver) == 0 {
		return mihomoSettings{}, errors.New("启用 DNS 后，请至少填写一个上游地址。")
	}
	return settings, nil
}

func validDNSUpstream(address string) bool {
	if address == "system" || address == "system://" {
		return true
	}
	if ip, err := netip.ParseAddr(address); err == nil {
		return ip.Zone() == ""
	}
	if strings.ContainsAny(address, "#?") {
		return false
	}
	if !strings.Contains(address, "://") {
		address = "udp://" + address
	}
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.User != nil || strings.HasSuffix(endpoint.Host, ":") {
		return false
	}
	host, valid := normalizedHost(endpoint.Hostname())
	if !valid || (strings.Contains(host, ":") && !strings.HasPrefix(endpoint.Host, "[")) {
		return false
	}
	if port := endpoint.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return false
		}
	}
	switch endpoint.Scheme {
	case "http", "https":
		return true
	case "udp", "tcp", "tls", "quic":
		return endpoint.Path == "" || endpoint.Path == "/"
	default:
		return false
	}
}
