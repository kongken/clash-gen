package http

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	fixtureUUID      = "11111111-1111-4111-8111-111111111111"
	fixturePublicKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"
)

func TestMixedProtocolDraftAndPreview(t *testing.T) {
	router, cookie, path := newDraftEditor(t, "多协议订阅")
	form := mixedProtocolForm("多协议订阅", "1")
	form["node_skip_cert_verify"] = []string{"true", "true", "true", "", "false", "", "", "", "true", "true", "", "true", "false"}
	names := form["node_name"]
	if save := authRequest(router, http.MethodPost, path, form, cookie); save.Code != http.StatusSeeOther {
		t.Fatalf("save mixed protocol draft returned %d", save.Code)
	}
	edit := authRequest(router, http.MethodGet, path, nil, cookie)
	if edit.Code != http.StatusOK || !strings.Contains(edit.Body.String(), `name="node_name" value="Trojan WS"`) ||
		!strings.Contains(edit.Body.String(), `value="reality" selected`) || !strings.Contains(edit.Body.String(), `value="xtls-rprx-vision" selected`) ||
		!strings.Contains(edit.Body.String(), `name="node_ws_path" value="/vless?lane=1"`) ||
		strings.Count(edit.Body.String(), `<option value="true" selected>跳过验证`) != 3 {
		t.Fatalf("saved protocol inputs were not restored by editor (status %d)", edit.Code)
	}

	form.Set("version", "2")
	preview := authRequest(router, http.MethodPost, path+"/preview", form, cookie)
	if preview.Code != http.StatusOK {
		t.Fatalf("mixed protocol preview returned %d %s", preview.Code, preview.Body.String())
	}
	output := yamlFromPreview(t, preview.Body.String())
	var config struct {
		Proxies []map[string]any
		Groups  []struct {
			Name, Type string
			Proxies    []string
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(output, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Groups) != 1 || config.Groups[0].Name != "PROXY" || config.Groups[0].Type != "select" ||
		!reflect.DeepEqual(config.Groups[0].Proxies, names) || len(config.Proxies) != len(names) {
		t.Fatalf("mixed protocol names or PROXY select group invalid: %+v", config.Groups)
	}
	checks := map[string]map[string]any{
		"Shadowsocks":   {"type": "ss", "password": "fictional-ss", "cipher": "aes-128-gcm"},
		"VMess TCP":     {"network": "tcp", "type": "vmess", "tls": false, "uuid": fixtureUUID, "alterId": 0, "cipher": "auto"},
		"VMess TCP TLS": {"network": "tcp", "tls": true, "servername": "vmess.example.test"},
		"VMess WS":      {"network": "ws", "tls": false, "alterId": 16, "cipher": "aes-128-gcm", "ws-opts": map[string]any{"path": "/data"}},
		"VMess WS TLS":  {"network": "ws", "tls": true, "ws-opts": map[string]any{"path": "/vmess", "headers": map[string]any{"Host": "front.example.test:8443"}}},
		"VLESS TCP":     {"network": "tcp", "type": "vless", "tls": false, "uuid": fixtureUUID},
		"VLESS TCP TLS": {"network": "tcp", "tls": true},
		"VLESS WS":      {"network": "ws", "tls": false, "ws-opts": map[string]any{"path": "/"}},
		"VLESS WS TLS":  {"network": "ws", "tls": true, "ws-opts": map[string]any{"path": "/vless?lane=1"}},
		"VLESS REALITY": {"network": "tcp", "tls": true, "flow": nil, "client-fingerprint": "chrome", "servername": "cover.example.test", "reality-opts": map[string]any{"public-key": fixturePublicKey, "short-id": ""}},
		"VLESS Vision":  {"network": "tcp", "tls": true, "flow": "xtls-rprx-vision", "reality-opts": map[string]any{"public-key": fixturePublicKey, "short-id": "0123abcd"}},
		"Trojan TCP":    {"network": "tcp", "type": "trojan", "password": "fictional-trojan", "sni": "trojan.example.test"},
		"Trojan WS":     {"network": "ws", "type": "trojan", "password": "fictional-trojan-ws", "ws-opts": map[string]any{"path": "/trojan", "headers": map[string]any{"Host": "trojan-ws.example.test"}}},
	}
	for index, proxy := range config.Proxies {
		if proxy["name"] != names[index] {
			t.Fatalf("proxy %d has unexpected name %v", index, proxy["name"])
		}
		for key, want := range checks[names[index]] {
			if !reflect.DeepEqual(proxy[key], want) {
				t.Errorf("proxy %q: %s = %#v, want %#v", names[index], key, proxy[key], want)
			}
		}
		value, present := proxy["skip-cert-verify"]
		wantSkip := names[index] == "VMess TCP TLS" || names[index] == "VLESS WS TLS" || names[index] == "Trojan TCP"
		if present != wantSkip || (present && value != true) {
			t.Errorf("proxy %q: skip-cert-verify = %v (present %t), want enabled %t", names[index], value, present, wantSkip)
		}
	}
	verifyMihomoYAML(t, output)
}

func mixedProtocolForm(name, version string) url.Values {
	names := []string{
		"Shadowsocks", "VMess TCP", "VMess TCP TLS", "VMess WS", "VMess WS TLS",
		"VLESS TCP", "VLESS TCP TLS", "VLESS WS", "VLESS WS TLS", "VLESS REALITY", "VLESS Vision",
		"Trojan TCP", "Trojan WS",
	}
	form := url.Values{
		"name": {name}, "version": {version}, "node_name": names,
		"node_type":             {"ss", "vmess", "vmess", "vmess", "vmess", "vless", "vless", "vless", "vless", "vless", "vless", "trojan", "trojan"},
		"node_network":          {"", "tcp", "tcp", "ws", "ws", "tcp", "tcp", "ws", "ws", "tcp", "tcp", "tcp", "ws"},
		"node_security":         {"", "", "tls", "", "tls", "", "tls", "", "tls", "reality", "reality", "", ""},
		"node_sni":              {"", "", "vmess.example.test", "", "", "", "", "", "", "cover.example.test", "cover.example.test", "trojan.example.test", ""},
		"node_password":         {"fictional-ss", "", "", "", "", "", "", "", "", "", "", "fictional-trojan", "fictional-trojan-ws"},
		"node_cipher":           {"aes-128-gcm", "", "", "", "", "", "", "", "", "", "", "", ""},
		"node_ws_path":          {"", "", "", "/data", "/vmess", "", "", "", "/vless?lane=1", "", "", "", "/trojan"},
		"node_ws_host":          {"", "", "", "", "front.example.test:8443", "", "", "", "", "", "", "", "trojan-ws.example.test"},
		"node_alter_id":         {"", "", "", "16", "0", "", "", "", "", "", "", "", ""},
		"node_vmess_cipher":     {"", "", "", "aes-128-gcm", "", "", "", "", "", "", "", "", ""},
		"node_reality_key":      {"", "", "", "", "", "", "", "", "", fixturePublicKey, fixturePublicKey, "", ""},
		"node_reality_short_id": {"", "", "", "", "", "", "", "", "", "", "0123abcd", "", ""},
		"node_flow":             {"", "", "", "", "", "", "", "", "", "", "xtls-rprx-vision", "", ""},
	}
	for index, nodeType := range form["node_type"] {
		form.Add("node_server", "192.0.2.10")
		form.Add("node_port", "443")
		form.Add("node_uuid", "")
		if nodeType == "vmess" || nodeType == "vless" {
			form["node_uuid"][index] = fixtureUUID
		}
	}
	return form
}

func TestProtocolPreviewValidatesModeSpecificFields(t *testing.T) {
	router, cookie, path := newDraftEditor(t, "未完成的节点")
	for _, tc := range []struct {
		name     string
		fields   map[string]string
		wantText string
	}{
		{"VMess UUID", map[string]string{"type": "vmess", "uuid": "invalid"}, "请输入有效的 UUID"},
		{"VMess Alter ID", map[string]string{"type": "vmess", "alter_id": "-1"}, "VMess Alter ID 必须是 0 至 65535 的整数"},
		{"VLESS WebSocket path", map[string]string{"type": "vless", "network": "ws", "ws_path": "missing-slash"}, "WebSocket 路径必须以 / 开头"},
		{"VLESS REALITY transport", map[string]string{"type": "vless", "network": "ws", "security": "reality"}, "REALITY 需要使用 TCP"},
		{"VLESS REALITY public key", map[string]string{"type": "vless", "security": "reality"}, "请输入有效的 REALITY 公钥"},
		{"VLESS REALITY short ID", map[string]string{"type": "vless", "security": "reality", "reality_key": fixturePublicKey, "reality_short_id": "1"}, "REALITY Short ID 必须为偶数位"},
		{"VLESS REALITY SNI", map[string]string{"type": "vless", "security": "reality", "reality_key": fixturePublicKey}, "请输入 REALITY 的 SNI"},
		{"Trojan password", map[string]string{"type": "trojan"}, "请输入 Trojan 密码"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{
				"name": {"未完成的节点"}, "version": {"1"}, "node_name": {"节点"},
				"node_server": {"192.0.2.10"}, "node_port": {"443"}, "node_uuid": {fixtureUUID},
			}
			for field, value := range tc.fields {
				form.Set("node_"+field, value)
			}
			preview := authRequest(router, http.MethodPost, path+"/preview", form, cookie)
			if preview.Code != http.StatusUnprocessableEntity || !strings.Contains(preview.Body.String(), "节点 1："+tc.wantText) {
				t.Errorf("preview status %d with no field-specific error %q", preview.Code, tc.wantText)
			}
		})
	}
}
