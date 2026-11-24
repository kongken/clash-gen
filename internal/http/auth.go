package http

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	adminCookie        = "clash_gen_session"
	sessionTTL         = 24 * time.Hour
	adminCredentialKey = "clash-gen:{admin}:credential"
)

//go:embed templates/login.html
var loginHTML string

var loginTemplate = template.Must(template.New("login").Parse(loginHTML))

type adminAuth struct {
	tokenDigest  string
	generation   string
	redis        redis.UniversalClient
	secureCookie bool
}

// A different configured token rotates the generation atomically. Returning to
// a prior token therefore creates a new generation for that token as well.
var configureAdminCredential = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
local prefix = ARGV[1] .. ":"
if current and string.sub(current, 1, string.len(prefix)) == prefix then
    return current
end
local replacement = prefix .. ARGV[2]
redis.call("SET", KEYS[1], replacement)
return replacement
`)

func registerAdminAuth(r *gin.Engine, options PanelOptions) error {
	auth := &adminAuth{
		tokenDigest:  authDigest(options.AdminToken),
		redis:        options.Redis,
		secureCookie: options.SecureCookie,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	generation, err := configureAdminCredential.Run(ctx, auth.redis, []string{adminCredentialKey}, auth.tokenDigest, rand.Text()).Text()
	if err != nil {
		return errors.New("could not initialize administrator session state in Redis")
	}
	auth.generation = generation

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/admin")
	})
	r.GET("/login", sensitiveRequest, func(c *gin.Context) {
		renderLogin(c, http.StatusOK, "")
	})
	r.POST("/login", sensitiveRequest, requireSameOrigin, auth.login)
	admin := r.Group("/admin", sensitiveRequest, requireSameOrigin, auth.requireSession)
	admin.POST("/logout", auth.logout)
	registerSubscriptions(r, admin, options)
	return nil
}

func sensitiveRequest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")

	// Gin's default recovery can include the request URI and cookies in the log.
	defer func() {
		if recover() != nil {
			slog.Error("panic serving sensitive HTTP route", "route", c.FullPath())
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}()
	c.Next()
}

func requireSameOrigin(c *gin.Context) {
	var protection http.CrossOriginProtection
	if err := protection.Check(c.Request); err != nil {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Next()
}

func renderLogin(c *gin.Context, status int, message string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_ = loginTemplate.Execute(c.Writer, message)
}

func (a *adminAuth) login(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.Request.ParseForm(); err != nil {
		renderLogin(c, http.StatusBadRequest, "登录请求无效")
		return
	}
	digest := authDigest(c.Request.PostForm.Get("token"))
	if subtle.ConstantTimeCompare([]byte(digest), []byte(a.tokenDigest)) != 1 {
		renderLogin(c, http.StatusUnauthorized, "管理 Token 不正确")
		return
	}

	ctx := c.Request.Context()
	active, err := a.redis.Get(ctx, adminCredentialKey).Result()
	if err != nil || active != a.generation {
		renderLogin(c, http.StatusServiceUnavailable, "暂时无法登录，请重试")
		return
	}
	session := rand.Text()
	if err := a.redis.Set(ctx, authSessionKey(session), a.generation, sessionTTL).Err(); err != nil {
		renderLogin(c, http.StatusServiceUnavailable, "暂时无法登录，请重试")
		return
	}

	a.setCookie(c, session, int(sessionTTL.Seconds()), time.Now().Add(sessionTTL))
	c.Redirect(http.StatusSeeOther, "/admin")
}

func (a *adminAuth) requireSession(c *gin.Context) {
	session, err := c.Cookie(adminCookie)
	if err != nil {
		requireLogin(c)
		return
	}
	values, err := a.redis.MGet(c.Request.Context(), authSessionKey(session), adminCredentialKey).Result()
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	if values[0] != a.generation || values[1] != a.generation {
		requireLogin(c)
		return
	}
	c.Next()
}

func requireLogin(c *gin.Context) {
	c.Abort()
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	c.String(http.StatusUnauthorized, "请先登录")
}

func (a *adminAuth) logout(c *gin.Context) {
	session, _ := c.Cookie(adminCookie) // The session has already been authenticated.
	if err := a.redis.Del(c.Request.Context(), authSessionKey(session)).Err(); err != nil {
		c.String(http.StatusServiceUnavailable, "暂时无法登出，请重试")
		return
	}
	a.setCookie(c, "", -1, time.Unix(0, 0))
	c.Redirect(http.StatusSeeOther, "/login")
}

func (a *adminAuth) setCookie(c *gin.Context, session string, maxAge int, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     adminCookie,
		Value:    session,
		Path:     "/admin",
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: true,
		Secure:   a.secureCookie || c.Request.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func authDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func authSessionKey(session string) string {
	return "clash-gen:{admin}:session:" + authDigest(session)
}
