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
	draft := subscription.Draft{Name: form.Get("name"), Nodes: nodes}
	if err := readRulesAndSettings(form, &draft); err != nil {
		return 0, subscription.Draft{}, err
	}
	return version, draft, nil
}

func readNodes(form url.Values) ([]subscription.Node, error) {
	count := len(form["node_name"])
	for _, field := range []string{"node_type", "node_server", "node_port", "node_cipher", "node_password"} {
		if length := len(form[field]); length != 0 && length != count {
			return nil, errors.New("节点表单字段不完整，请刷新后重试。")
		}
	}
	if count == 0 {
		return nil, nil
	}

	nodes := make([]subscription.Node, count)
	for index, name := range form["node_name"] {
		nodes[index] = subscription.Node{
			Name:     name,
			Type:     formRowValue(form, "node_type", index),
			Server:   formRowValue(form, "node_server", index),
			Port:     formRowValue(form, "node_port", index),
			Cipher:   formRowValue(form, "node_cipher", index),
			Password: formRowValue(form, "node_password", index),
		}
		if nodes[index].Type == "" {
			nodes[index].Type = "ss"
		}
	}
	return nodes, nil
}

func formRowValue(form url.Values, field string, index int) string {
	if values := form[field]; index < len(values) {
		return values[index]
	}
	return ""
}
