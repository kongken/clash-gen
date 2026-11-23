package main

import (
	"log/slog"

	"butterfly.orx.me/core/app"
	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/config"
	apihttp "github.com/kongken/clash-gen/internal/http"
)

func main() {
	svcConfig := &config.ServiceConfig{}

	appConfig := &app.Config{
		Service:   "clash-gen",
		Namespace: "kongken",
		Config:    svcConfig,
		Router: func(r *gin.Engine) {
			apihttp.RegisterRoutes(r)
		},
		InitFunc: []func() error{
			func() error {
				slog.Info("clash-gen ready", "environment", svcConfig.Environment)
				return nil
			},
		},
	}

	app.New(appConfig).Run()
}
