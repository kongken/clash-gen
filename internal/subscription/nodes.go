package subscription

import (
	"errors"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// Node keeps the entered values, including incomplete port and credential fields.
type Node struct {
	Type           string `json:"type"`
	Name           string `json:"name"`
	Server         string `json:"server"`
	Port           string `json:"port"`
	Cipher         string `json:"cipher"`
	Password       string `json:"password"`
	Network        string `json:"network,omitempty"`
	Security       string `json:"security,omitempty"`
	SkipCertVerify bool   `json:"skip_cert_verify,omitempty"`
	UUID           string `json:"uuid,omitempty"`
	AlterID        string `json:"alter_id,omitempty"`
	VMessCipher    string `json:"vmess_cipher,omitempty"`
	ServerName     string `json:"server_name,omitempty"`
	WSPath         string `json:"ws_path,omitempty"`
	WSHost         string `json:"ws_host,omitempty"`
	RealityKey     string `json:"reality_key,omitempty"`
	RealityShortID string `json:"reality_short_id,omitempty"`
	Flow           string `json:"flow,omitempty"`
}

func ShadowsocksCiphers() []string {
	return []string{"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305"}
}

func (n Node) proxy() (map[string]any, error) {
	name := strings.TrimSpace(n.Name)
	if name == "" || strings.ContainsFunc(name, unicode.IsControl) {
		return nil, errors.New("请输入有效的节点名称。")
	}
	switch strings.ToUpper(name) {
	case "PROXY", "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL":
		return nil, errors.New("节点名称与 Mihomo 策略名称冲突。")
	}
	server, ok := normalizedHost(n.Server)
	if !ok {
		return nil, errors.New("服务器必须是有效的域名或 IP 地址。")
	}
	port, err := strconv.ParseUint(strings.TrimSpace(n.Port), 10, 16)
	if err != nil || port == 0 {
		return nil, errors.New("端口必须是 1 至 65535 的整数。")
	}

	nodeType := n.Type
	if nodeType == "" {
		nodeType = "ss"
	}
	proxy := map[string]any{"name": name, "type": nodeType, "server": server, "port": port}
	switch nodeType {
	case "ss":
		if !slices.Contains(ShadowsocksCiphers(), n.Cipher) {
			return nil, errors.New("请选择支持的 Shadowsocks 加密方式。")
		}
		if n.Password == "" {
			return nil, errors.New("请输入 Shadowsocks 密码。")
		}
		proxy["cipher"] = n.Cipher
		proxy["password"] = n.Password
	case "vmess", "vless", "trojan":
		if err := n.addTunnelOptions(proxy); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("请选择支持的节点协议。")
	}
	return proxy, nil
}

var dnsLabel = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func normalizedHost(value string) (string, bool) {
	host := strings.TrimSpace(value)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
		ip, err := netip.ParseAddr(host)
		return host, err == nil && ip.Is6() && ip.Zone() == ""
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return host, ip.Zone() == ""
	}
	ascii, err := idna.Lookup.ToASCII(host)
	labels := strings.TrimSuffix(ascii, ".")
	if err != nil || labels == "" || len(labels) > 253 {
		return "", false
	}
	for label := range strings.SplitSeq(labels, ".") {
		if !dnsLabel.MatchString(label) {
			return "", false
		}
	}
	return ascii, true
}
