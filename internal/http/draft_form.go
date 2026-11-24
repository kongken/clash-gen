package http

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/subscription"
)

func parseDraftForm(c *gin.Context) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	return c.Request.ParseForm()
}

func readDraftPost(c *gin.Context) (uint64, subscription.Draft, error) {
	if err := parseDraftForm(c); err != nil {
		return 0, subscription.Draft{}, errors.New("草稿请求无效。")
	}
	form := c.Request.PostForm
	version, err := strconv.ParseUint(form.Get("version"), 10, 64)
	if err != nil || version == 0 {
		return 0, subscription.Draft{}, errors.New("草稿版本无效，请刷新后重试。")
	}

	nodes, err := readNodes(form)
	if err != nil {
		return 0, subscription.Draft{}, err
	}
	return version, subscription.Draft{Name: form.Get("name"), Nodes: nodes}, nil
}

func readNodes(form url.Values) ([]subscription.Node, error) {
	count := len(form["node_name"])
	for _, field := range []string{
		"node_type", "node_server", "node_port", "node_cipher", "node_password",
		"node_network", "node_security", "node_uuid", "node_alter_id", "node_vmess_cipher",
		"node_sni", "node_ws_path", "node_ws_host", "node_reality_key", "node_reality_short_id", "node_flow",
	} {
		if length := len(form[field]); length != 0 && length != count {
			return nil, errors.New("节点表单字段不完整，请刷新后重试。")
		}
	}
	if count == 0 {
		return nil, nil
	}

	nodes := make([]subscription.Node, count)
	for index, name := range form["node_name"] {
		value := func(field string) string { return formRowValue(form, "node_"+field, index) }
		node := subscription.Node{
			Name:   name,
			Type:   value("type"),
			Server: value("server"),
			Port:   value("port"),
		}
		if node.Type == "" {
			node.Type = "ss"
		}
		switch node.Type {
		case "ss":
			node.Cipher = value("cipher")
			node.Password = value("password")
		case "vmess", "vless", "trojan":
			node.Network = value("network")
			if node.Network == "ws" {
				node.WSPath = value("ws_path")
				node.WSHost = value("ws_host")
			}
			if node.Type == "trojan" {
				node.Password = value("password")
				node.ServerName = value("sni")
			} else {
				node.UUID = value("uuid")
				node.Security = value("security")
				if node.Security != "" {
					node.ServerName = value("sni")
				}
				if node.Security == "reality" {
					node.RealityKey = value("reality_key")
					node.RealityShortID = value("reality_short_id")
					node.Flow = value("flow")
				}
			}
			if node.Type == "vmess" {
				node.AlterID = value("alter_id")
				node.VMessCipher = value("vmess_cipher")
			}
		}
		nodes[index] = node
	}
	return nodes, nil
}

func formRowValue(form url.Values, field string, index int) string {
	if values := form[field]; index < len(values) {
		return values[index]
	}
	return ""
}
