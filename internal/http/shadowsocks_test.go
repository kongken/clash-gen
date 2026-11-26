package http

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func TestShadowsocksYAMLPreview(t *testing.T) {
	router, cookie, path := newDraftEditor(t, "家庭订阅")
	form := url.Values{
		"name": {"家庭订阅"}, "version": {"1"},
		"node_type": {"ss", "ss"}, "node_name": {"备用 <节点>", "主要节点"},
		"node_server": {"ss-alt.example.test", "192.0.2.15"}, "node_port": {"8443", "443"},
		"node_cipher":   {"chacha20-ietf-poly1305", "aes-256-gcm"},
		"node_password": {"fictional-backup", "fictional-primary"},
	}
	preview := authRequest(router, http.MethodPost, path+"/preview", form, cookie)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview returned %d %s", preview.Code, preview.Body.String())
	}
	output := yamlFromPreview(t, preview.Body.String())
	var config struct {
		Mode      string `yaml:"mode"`
		AllowLAN  *bool  `yaml:"allow-lan"`
		MixedPort int    `yaml:"mixed-port"`
		Proxies   []struct {
			Name, Type, Server, Cipher, Password string
			Port                                 int
		}
		Groups []struct {
			Name, Type string
			Proxies    []string
		} `yaml:"proxy-groups"`
		Rules []string
	}
	if err := yaml.Unmarshal(output, &config); err != nil {
		t.Fatal(err)
	}
	if config.Mode != "rule" || config.AllowLAN == nil || *config.AllowLAN || config.MixedPort == 0 {
		t.Fatalf("invalid base settings: %+v", config)
	}
	if len(config.Proxies) != 2 || config.Proxies[0].Name != "备用 <节点>" ||
		config.Proxies[0].Server != "ss-alt.example.test" || config.Proxies[0].Port != 8443 ||
		config.Proxies[0].Cipher != "chacha20-ietf-poly1305" || config.Proxies[0].Type != "ss" ||
		config.Proxies[0].Password != "fictional-backup" || config.Proxies[1].Name != "主要节点" {
		t.Fatalf("invalid proxies: %+v", config.Proxies)
	}
	if len(config.Groups) != 1 || config.Groups[0].Name != "PROXY" || config.Groups[0].Type != "select" ||
		!reflect.DeepEqual(config.Groups[0].Proxies, []string{"备用 <节点>", "主要节点"}) ||
		!reflect.DeepEqual(config.Rules, []string{"MATCH,PROXY"}) {
		t.Fatalf("invalid proxy group or rules: %+v %+v", config.Groups, config.Rules)
	}
	verifyMihomoYAML(t, output)
}

func TestIncompleteNodesAreEditableWithoutPublishingPreview(t *testing.T) {
	router, cookie, path := newDraftEditor(t, "家庭订阅")
	form := url.Values{
		"name": {"家庭订阅"}, "version": {"1"},
		"node_name":   {"备用节点", "主要节点"},
		"node_server": {"secondary.example.test", "primary.example.test"}, "node_port": {"443", "8443"},
		"node_cipher": {"aes-128-gcm", "aes-192-gcm"}, "node_password": {"", "fictional-primary"},
	}
	if save := authRequest(router, http.MethodPost, path, form, cookie); save.Code != http.StatusSeeOther {
		t.Fatalf("save incomplete nodes returned %d %q", save.Code, save.Body.String())
	}
	edit := authRequest(router, http.MethodGet, path, nil, cookie)
	first := strings.Index(edit.Body.String(), `name="node_name" value="备用节点"`)
	second := strings.Index(edit.Body.String(), `name="node_name" value="主要节点"`)
	if edit.Code != http.StatusOK || first < 0 || second <= first {
		t.Fatalf("saved nodes were missing or out of order in editor: %d %q", edit.Code, edit.Body.String())
	}

	form.Set("version", "2")
	invalid := authRequest(router, http.MethodPost, path+"/preview", form, cookie)
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), "节点 1：请输入 Shadowsocks 密码") {
		t.Fatalf("preview did not identify the missing password: %d %q", invalid.Code, invalid.Body.String())
	}

	updated := url.Values{
		"name": {"家庭订阅"}, "version": {"2"},
		"node_name": {"更新后节点"}, "node_server": {"updated.example.test"}, "node_port": {"8443"},
		"node_cipher": {"aes-192-gcm"}, "node_password": {"fictional-update"},
	}
	preview := authRequest(router, http.MethodPost, path+"/preview", updated, cookie)
	if preview.Code != http.StatusOK || !strings.Contains(string(yamlFromPreview(t, preview.Body.String())), "更新后节点") {
		t.Fatalf("preview returned %d %q", preview.Code, preview.Body.String())
	}
	edit = authRequest(router, http.MethodGet, path, nil, cookie)
	if !strings.Contains(edit.Body.String(), "备用节点") || strings.Contains(edit.Body.String(), "更新后节点") {
		t.Fatal("preview changed the persisted draft")
	}

	save := authRequest(router, http.MethodPost, path, updated, cookie)
	if save.Code != http.StatusSeeOther {
		t.Fatalf("save edited nodes returned %d %q", save.Code, save.Body.String())
	}
	edit = authRequest(router, http.MethodGet, path, nil, cookie)
	if !strings.Contains(edit.Body.String(), "更新后节点") || strings.Contains(edit.Body.String(), "备用节点") {
		t.Fatal("editor did not restore the updated draft")
	}
}

func newDraftEditor(t *testing.T, name string) (*gin.Engine, *http.Cookie, string) {
	t.Helper()
	router, _, _ := newAuthRouter(t)
	login := authRequest(router, http.MethodPost, "/login", url.Values{"token": {"test-admin-token"}}, nil)
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login returned %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	created := authRequest(router, http.MethodPost, "/admin/subscriptions", url.Values{"name": {name}}, cookie)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create subscription returned %d", created.Code)
	}
	return router, cookie, created.Header().Get("Location")
}

func yamlFromPreview(t *testing.T, page string) []byte {
	t.Helper()
	_, preview, found := strings.Cut(page, `<pre id="yaml-preview">`)
	if !found {
		t.Fatal("YAML preview is missing")
	}
	preview, _, found = strings.Cut(preview, "</pre>")
	if !found {
		t.Fatal("YAML preview is incomplete")
	}
	return []byte(html.UnescapeString(preview))
}

func verifyMihomoYAML(t *testing.T, output []byte) {
	t.Helper()
	executable := os.Getenv("CLASH_GEN_TEST_MIHOMO")
	if executable == "" {
		return
	}
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configFile, output, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-t", "-d", configDir, "-f", configFile)
	if log, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Mihomo validation failed: %v\n%s", err, log)
	}
}
