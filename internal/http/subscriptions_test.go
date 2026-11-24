package http

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestAdminDraftsPersistAndStayIsolated(t *testing.T) {
	router, server, _ := newAuthRouter(t)
	cookie := authRequest(router, http.MethodPost, "/login", url.Values{"token": {"test-admin-token"}}, nil).Result().Cookies()[0]
	create := func(name string) string {
		t.Helper()
		response := authRequest(router, http.MethodPost, "/admin/subscriptions", url.Values{"name": {name}}, cookie)
		location := response.Header().Get("Location")
		if response.Code != http.StatusSeeOther || !strings.HasPrefix(location, "/admin/subscriptions/") {
			t.Fatalf("create %q returned %d and location %q", name, response.Code, location)
		}
		return location
	}

	if response := authRequest(router, http.MethodPost, "/admin/subscriptions", url.Values{"name": {"未授权"}}, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous create returned %d", response.Code)
	}
	first := create("个人订阅")
	second := create("测试订阅")
	if first == second {
		t.Fatal("separate subscriptions share an edit page")
	}
	edit := authRequest(router, http.MethodGet, first, nil, cookie)
	if edit.Code != http.StatusOK || !strings.Contains(edit.Body.String(), "个人订阅") || !strings.Contains(edit.Body.String(), "未发布") {
		t.Fatalf("new draft edit page returned %d %q", edit.Code, edit.Body.String())
	}

	change := url.Values{"name": {"家庭订阅"}, "version": {"1"}}
	if response := authRequest(router, http.MethodPost, first, change, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous update returned %d", response.Code)
	}
	if response := authRequest(router, http.MethodPost, first, change, cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("save draft returned %d %q", response.Code, response.Body.String())
	}
	if response := authRequest(router, http.MethodPost, first, url.Values{"name": {"冲突更名"}, "version": {"1"}}, cookie); response.Code != http.StatusConflict {
		t.Fatalf("saving over a newer draft returned %d", response.Code)
	}

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	restarted := gin.New()
	RegisterRoutes(restarted, PanelOptions{AdminToken: "test-admin-token", Redis: client})
	list := authRequest(restarted, http.MethodGet, "/admin", nil, cookie)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "订阅列表") ||
		!strings.Contains(list.Body.String(), "家庭订阅") || !strings.Contains(list.Body.String(), "测试订阅") ||
		strings.Contains(list.Body.String(), "个人订阅") || strings.Contains(list.Body.String(), "未授权") {
		t.Fatalf("subscription list after restarting returned %d %q", list.Code, list.Body.String())
	}
	edit = authRequest(restarted, http.MethodGet, second, nil, cookie)
	if edit.Code != http.StatusOK || !strings.Contains(edit.Body.String(), "测试订阅") || strings.Contains(edit.Body.String(), "家庭订阅") {
		t.Fatalf("separate draft was changed: %d %q", edit.Code, edit.Body.String())
	}
}
