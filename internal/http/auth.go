package http

import (
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
	adminCookie = "clash_gen_session"
	sessionTTL  = 24 * time.Hour
)

//go:embed templates/login.html
var loginHTML string

var loginTemplate = template.Must(template.New("login").Parse(loginHTML))

type adminAuth struct {
	tokenDigest  string
	redis        redis.UniversalClient
	secureCookie bool
}

func registerAdminAuth(r *gin.Engine, options PanelOptions) {
	auth := &adminAuth{
		tokenDigest:  authDigest(options.AdminToken),
		redis:        options.Redis,
		secureCookie: options.SecureCookie,
	}

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

	session := rand.Text()
	if err := a.redis.Set(c.Request.Context(), authSessionKey(session), a.tokenDigest, sessionTTL).Err(); err != nil {
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
	digest, err := a.redis.Get(c.Request.Context(), authSessionKey(session)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	if subtle.ConstantTimeCompare([]byte(digest), []byte(a.tokenDigest)) != 1 {
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
	return "clash-gen:admin-session:" + authDigest(session)
}
