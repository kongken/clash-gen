package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestAdminLogin(t *testing.T) {
	r, _, _ := newAuthRouter(t)
	page := authRequest(r, http.MethodGet, "/login", nil, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "管理员登录") {
		t.Fatalf("login page = %d %q", page.Code, page.Body.String())
	}
	blocked := authRequest(r, http.MethodGet, "/admin", nil, nil)
	if blocked.Code != http.StatusSeeOther || blocked.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated admin page = %d, %s", blocked.Code, blocked.Header().Get("Location"))
	}
	wrong := authRequest(r, http.MethodPost, "/login", url.Values{"token": {"wrong"}}, nil)
	if wrong.Code != http.StatusUnauthorized || len(wrong.Result().Cookies()) != 0 {
		t.Fatalf("incorrect token = %d, cookies = %d", wrong.Code, len(wrong.Result().Cookies()))
	}
	login := authRequest(r, http.MethodPost, "/login", url.Values{"token": {"test-admin-token"}}, nil)
	cookies := login.Result().Cookies()
	if login.Code != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatalf("login = %d, cookies = %d", login.Code, len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.MaxAge != 86400 {
		t.Fatalf("login cookie flags = HttpOnly %v, Secure %v, MaxAge %d", cookie.HttpOnly, cookie.Secure, cookie.MaxAge)
	}
	page = authRequest(r, http.MethodGet, "/admin", nil, cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("authenticated admin page = %d", page.Code)
	}
}

func TestAdminLogoutRevokesOnlyCurrentSession(t *testing.T) {
	r, _, _ := newAuthRouter(t)
	token := url.Values{"token": {"test-admin-token"}}
	first := authRequest(r, http.MethodPost, "/login", token, nil).Result().Cookies()[0]
	second := authRequest(r, http.MethodPost, "/login", token, nil).Result().Cookies()[0]

	anonymous := authRequest(r, http.MethodPost, "/admin/logout", nil, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated logout = %d", anonymous.Code)
	}

	crossSite := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	crossSite.AddCookie(first)
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, crossSite)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site logout = %d", rec.Code)
	}

	logout := authRequest(r, http.MethodPost, "/admin/logout", nil, first)
	if logout.Code != http.StatusSeeOther || logout.Header().Get("Location") != "/login" {
		t.Fatalf("logout = %d, %s", logout.Code, logout.Header().Get("Location"))
	}
	if cleared := logout.Result().Cookies(); len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatal("logout did not clear the session cookie")
	}
	if page := authRequest(r, http.MethodGet, "/admin", nil, first); page.Code != http.StatusSeeOther {
		t.Fatalf("replayed logged-out cookie = %d", page.Code)
	}
	if page := authRequest(r, http.MethodGet, "/admin", nil, second); page.Code != http.StatusOK {
		t.Fatalf("other browser session = %d", page.Code)
	}
}

func TestAdminSessionExpiresOrRejectsChangedToken(t *testing.T) {
	r, server, client := newAuthRouter(t)
	token := url.Values{"token": {"test-admin-token"}}
	login := authRequest(r, http.MethodPost, "/login", token, nil)
	cookie := login.Result().Cookies()[0]

	rotated := gin.New()
	RegisterRoutes(rotated, PanelOptions{AdminToken: "rotated-admin-token", Redis: client})
	if page := authRequest(rotated, http.MethodGet, "/admin", nil, cookie); page.Code != http.StatusSeeOther {
		t.Fatalf("old cookie after changing the deployment token = %d", page.Code)
	}
	if login := authRequest(rotated, http.MethodPost, "/login", token, nil); login.Code != http.StatusUnauthorized {
		t.Fatalf("old token after changing the deployment token = %d", login.Code)
	}

	server.FastForward(24*time.Hour + time.Second)
	if page := authRequest(r, http.MethodGet, "/admin", nil, cookie); page.Code != http.StatusSeeOther {
		t.Fatalf("replayed expired cookie = %d", page.Code)
	}
}

func newAuthRouter(t *testing.T) (*gin.Engine, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	r := gin.New()
	RegisterRoutes(r, PanelOptions{AdminToken: "test-admin-token", Redis: client, SecureCookie: true})
	return r, server, client
}

func authRequest(r *gin.Engine, method, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
