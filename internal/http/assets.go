package http

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed assets/panel.css
var panelCSS []byte

func registerPanelAssets(r *gin.Engine) {
	r.GET("/assets/panel.css", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/css; charset=utf-8", panelCSS)
	})
}
