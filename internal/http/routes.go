// Package http contains the Gin REST routes for clash-gen.
package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes mounts all HTTP routes on r.
func RegisterRoutes(r *gin.Engine) {
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
}
