package http

import (
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDraftRulesAndSettingsInMihomoPreview(t *testing.T) {
	router, cookie, path := newDraftEditor(t, "分流设置")
	form := url.Values{
		"name": {"分流设置"}, "version": {"1"},
		"node_name": {"家庭节点"}, "node_server": {"ss.example.test"}, "node_port": {"443"},
		"node_cipher": {"aes-128-gcm"}, "node_password": {"fictional-password"},
		"rule_type":    {"DOMAIN", "DOMAIN-SUFFIX", "IP-CIDR", "IP-CIDR"},
		"rule_target":  {"REJECT", "PROXY", "DIRECT", "REJECT"},
		"rule_value":   {"ads.example.test", ".example.test", "192.0.2.100/24", "2001:db8:2::/48"},
		"match_target": {"DIRECT"}, "mixed_port": {"10808"}, "log_level": {"warning"},
		"dns_enabled": {"1"}, "dns_upstreams": {"192.0.2.53\r\n\r\nhttps://resolver.example.test/dns-query\r\n2001:db8::53"},
	}
	save := authRequest(router, http.MethodPost, path, form, cookie)
	if save.Code != http.StatusSeeOther {
		t.Fatalf("save custom settings returned %d %q", save.Code, save.Body.String())
	}
	loaded := authRequest(router, http.MethodGet, path, nil, cookie)
	if loaded.Code != http.StatusOK {
		t.Fatalf("load custom settings returned %d", loaded.Code)
	}
	page := loaded.Body.String()
	previous := -1
	for _, input := range []string{
		`name="rule_value" value="ads.example.test"`,
		`name="rule_value" value=".example.test"`,
		`name="rule_value" value="192.0.2.100/24"`,
		`name="rule_value" value="2001:db8:2::/48"`,
	} {
		if index := strings.Index(page, input); index > previous {
			previous = index
		} else {
			t.Fatalf("saved rule %q is missing or out of order", input)
		}
	}
	for _, value := range []string{`value="10808"`, `value="warning" selected`, `name="dns_enabled" value="1" checked`, "https://resolver.example.test/dns-query"} {
		if !strings.Contains(page, value) {
			t.Fatalf("saved setting %q is missing from editor (status %d)", value, loaded.Code)
		}
	}

	form.Set("version", "2")
	preview := authRequest(router, http.MethodPost, path+"/preview", form, cookie)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview custom settings returned %d %q", preview.Code, preview.Body.String())
	}
	output := yamlFromPreview(t, preview.Body.String())
	var config struct {
		Mode      string
		AllowLAN  *bool  `yaml:"allow-lan"`
		MixedPort int    `yaml:"mixed-port"`
		LogLevel  string `yaml:"log-level"`
		Rules     []string
		DNS       struct {
			Enable     bool
			Nameserver []string
		}
	}
	if err := yaml.Unmarshal(output, &config); err != nil {
		t.Fatal(err)
	}
	if config.Mode != "rule" || config.AllowLAN == nil || *config.AllowLAN || config.MixedPort != 10808 || config.LogLevel != "warning" {
		t.Fatalf("incorrect base settings: %+v", config)
	}
	if !config.DNS.Enable || !reflect.DeepEqual(config.DNS.Nameserver, []string{"192.0.2.53", "https://resolver.example.test/dns-query", "2001:db8::53"}) {
		t.Fatalf("incorrect DNS settings: %+v", config.DNS)
	}
	wantRules := []string{
		"DOMAIN,ads.example.test,REJECT",
		"DOMAIN-SUFFIX,example.test,PROXY",
		"IP-CIDR,192.0.2.0/24,DIRECT",
		"IP-CIDR6,2001:db8:2::/48,REJECT",
		"MATCH,DIRECT",
	}
	if !reflect.DeepEqual(config.Rules, wantRules) {
		t.Fatalf("rules = %q, want %q", config.Rules, wantRules)
	}
	verifyMihomoYAML(t, output)
}

func TestDraftRulesAndSettingsReportValidationErrors(t *testing.T) {
	router, cookie, path := newDraftEditor(t, "待编辑")
	base := url.Values{
		"name": {"待编辑"}, "version": {"1"},
		"node_name": {"主要节点"}, "node_server": {"server.example.test"}, "node_port": {"443"},
		"node_cipher": {"aes-128-gcm"}, "node_password": {"fictional-password"},
		"rule_type": {"IP-CIDR"}, "rule_value": {"192.0.2.0/24"}, "rule_target": {"PROXY"},
		"dns_enabled": {"1"}, "dns_upstreams": {"192.0.2.53"},
	}
	for _, test := range []struct {
		name, field, value, message string
	}{
		{"missing IP subnet", "rule_value", "", "规则 1：请输入有效的 IPv4 或 IPv6 网段"},
		{"invalid IP subnet", "rule_value", "2001:db8::1/300", "规则 1：请输入有效的 IPv4 或 IPv6 网段"},
		{"extra MATCH", "rule_type", "MATCH", "规则 1：请选择域名、域名后缀或 IP 网段规则"},
		{"invalid rule destination", "rule_target", "GLOBAL", "规则 1：去向必须是 PROXY、DIRECT 或 REJECT"},
		{"invalid MATCH destination", "match_target", "GLOBAL", "MATCH 去向必须是 PROXY、DIRECT 或 REJECT"},
		{"invalid mixed-port", "mixed_port", "70000", "mixed-port 必须是 1 至 65535 的整数"},
		{"invalid log level", "log_level", "trace", "请选择有效的日志级别"},
		{"no DNS upstream", "dns_upstreams", "", "启用 DNS 后，请至少填写一个上游地址"},
		{"invalid DNS upstream", "dns_upstreams", "192.0.2.53\nhttps://", "DNS 上游第 2 行：请输入有效的地址"},
	} {
		t.Run(test.name, func(t *testing.T) {
			form := maps.Clone(base)
			form.Set(test.field, test.value)
			preview := authRequest(router, http.MethodPost, path+"/preview", form, cookie)
			if preview.Code != http.StatusUnprocessableEntity ||
				!strings.Contains(preview.Body.String(), test.message) {
				t.Fatalf("preview returned %d without %q", preview.Code, test.message)
			}
		})
	}

	base.Set("rule_value", "")
	base.Set("mixed_port", "70000")
	if save := authRequest(router, http.MethodPost, path, base, cookie); save.Code != http.StatusSeeOther {
		t.Fatalf("saving incomplete rules and settings returned %d", save.Code)
	}
	reopened := authRequest(router, http.MethodGet, path, nil, cookie)
	if reopened.Code != http.StatusOK || !strings.Contains(reopened.Body.String(), `name="mixed_port" value="70000"`) ||
		!strings.Contains(reopened.Body.String(), `value="IP-CIDR" selected`) {
		t.Fatalf("incomplete rules and settings were not restored in editor (status %d)", reopened.Code)
	}
}
