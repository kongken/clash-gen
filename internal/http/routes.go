// Package http contains the Gin routes for clash-gen.
package http

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/subscription"
	"github.com/redis/go-redis/v9"
)

// PanelOptions configures the administrator panel.
type PanelOptions struct {
	AdminToken   string
	Redis        redis.UniversalClient
	SecureCookie bool
	BaseURL      *url.URL
	Publisher    *subscription.Publisher
}

// RegisterRoutes mounts all configured HTTP routes on r.
func RegisterRoutes(r *gin.Engine, options ...PanelOptions) error {
	if len(options) > 0 && options[0].Publisher != nil {
		// Butterfly installs its logger and tracing middleware before this
		// callback. Scrub the credential before those handlers see the URI.
		r.Handlers = append(gin.HandlersChain{scrubDownloadToken}, r.Handlers...)
		r.Use() // Rebuild Gin's cached no-route and no-method chains.
	}
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	if len(options) > 0 && options[0].AdminToken != "" && options[0].Redis != nil {
		registerPanelAssets(r)
		return registerAdminAuth(r, options[0])
	}
	return nil
}
