package subscription

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const visionFlow = "xtls-rprx-vision"

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func VMessCiphers() []string {
	return []string{"auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero"}
}

func (n Node) addTunnelOptions(proxy map[string]any) error {
	if n.Type == "trojan" {
		if n.Password == "" {
			return errors.New("请输入 Trojan 密码。")
		}
		proxy["password"] = n.Password
	} else {
		uuid := strings.TrimSpace(n.UUID)
		if !uuidPattern.MatchString(uuid) {
			return errors.New("请输入有效的 UUID。")
		}
		proxy["uuid"] = uuid
	}

	if n.Type == "vmess" {
		cipher := n.VMessCipher
		if cipher == "" {
			cipher = "auto"
		}
		if !slices.Contains(VMessCiphers(), cipher) {
			return errors.New("请选择支持的 VMess 加密方式。")
		}
		var alterID uint64
		if value := strings.TrimSpace(n.AlterID); value != "" {
			var err error
			alterID, err = strconv.ParseUint(value, 10, 16)
			if err != nil {
				return errors.New("VMess Alter ID 必须是 0 至 65535 的整数。")
			}
		}
		proxy["cipher"] = cipher
		proxy["alterId"] = alterID
	}

	network := n.Network
	if network == "" {
		network = "tcp"
	}
	if network != "tcp" && network != "ws" {
		return errors.New("请选择 TCP 或 WebSocket 传输。")
	}
	proxy["network"] = network
	if network == "ws" {
		options, err := n.webSocketOptions()
		if err != nil {
			return err
		}
		proxy["ws-opts"] = options
	}
	return n.addTLSOptions(proxy, network)
}

func (n Node) addTLSOptions(proxy map[string]any, network string) error {
	security := n.Security
	serverNameKey := "servername"
	if n.Type == "trojan" {
		security = "tls"
		serverNameKey = "sni"
	} else {
		proxy["tls"] = security == "tls" || security == "reality"
	}

	switch security {
	case "":
		return nil
	case "tls":
		if n.SkipCertVerify {
			proxy["skip-cert-verify"] = true
		}
	case "reality":
		if n.Type != "vless" || network != "tcp" {
			return errors.New("REALITY 需要使用 TCP 传输的 VLESS 节点。")
		}
		if err := n.addRealityOptions(proxy); err != nil {
			return err
		}
	default:
		return errors.New("请选择支持的传输安全方式。")
	}

	if value := strings.TrimSpace(n.ServerName); value != "" {
		serverName, ok := normalizedHost(value)
		if !ok {
			return errors.New("SNI 必须是有效的域名或 IP 地址。")
		}
		proxy[serverNameKey] = serverName
	} else if security == "reality" {
		return errors.New("请输入 REALITY 的 SNI。")
	}
	return nil
}

func (n Node) addRealityOptions(proxy map[string]any) error {
	publicKey := strings.TrimSpace(n.RealityKey)
	key, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil || len(key) != 32 {
		return errors.New("请输入有效的 REALITY 公钥。")
	}
	shortID := strings.TrimSpace(n.RealityShortID)
	if decoded, err := hex.DecodeString(shortID); err != nil || len(decoded) > 8 {
		return errors.New("REALITY Short ID 必须为偶数位十六进制字符，最多 16 位。")
	}
	if n.Flow != "" {
		if n.Flow != visionFlow {
			return errors.New("请选择支持的 VLESS Flow。")
		}
		proxy["flow"] = n.Flow
	}
	proxy["client-fingerprint"] = "chrome"
	proxy["reality-opts"] = map[string]any{"public-key": publicKey, "short-id": shortID}
	return nil
}

func (n Node) webSocketOptions() (map[string]any, error) {
	path := strings.TrimSpace(n.WSPath)
	if path == "" {
		path = "/"
	}
	if _, err := url.ParseRequestURI(path); err != nil || !strings.HasPrefix(path, "/") ||
		strings.ContainsRune(path, '#') || strings.ContainsFunc(path, unicode.IsSpace) {
		return nil, errors.New("WebSocket 路径必须以 / 开头，且不能包含空白或片段。")
	}
	options := map[string]any{"path": path}
	if value := strings.TrimSpace(n.WSHost); value != "" {
		host, ok := webSocketHost(value)
		if !ok {
			return nil, errors.New("WebSocket Host 必须是有效的域名或 IP 地址，可包含端口。")
		}
		options["headers"] = map[string]string{"Host": host}
	}
	return options, nil
}

func webSocketHost(value string) (string, bool) {
	if host, port, err := net.SplitHostPort(value); err == nil {
		normalized, valid := normalizedHost(host)
		number, portErr := strconv.ParseUint(port, 10, 16)
		if !valid || portErr != nil || number == 0 {
			return "", false
		}
		return net.JoinHostPort(normalized, strconv.FormatUint(number, 10)), true
	}
	if normalized, ok := normalizedHost(value); ok {
		if strings.ContainsRune(normalized, ':') {
			return "[" + normalized + "]", true
		}
		return normalized, true
	}
	return "", false
}
