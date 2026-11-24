package main

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"butterfly.orx.me/core/app"
	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/config"
	apihttp "github.com/kongken/clash-gen/internal/http"
	"github.com/redis/go-redis/v9"
)

func main() {
	svcConfig := &config.ServiceConfig{}
	var panel apihttp.PanelOptions

	appConfig := &app.Config{
		Service:   "clash-gen",
		Namespace: "kongken",
		Config:    svcConfig,
		Router: func(r *gin.Engine) {
			apihttp.RegisterRoutes(r, panel)
		},
		InitFunc: []func() error{
			func() error {
				var err error
				panel, err = initPanel(svcConfig)
				if err != nil {
					return err
				}
				slog.Info("clash-gen ready", "environment", svcConfig.Environment)
				return nil
			},
		},
	}

	app.New(appConfig).Run()
	if panel.Redis != nil {
		_ = panel.Redis.Close()
	}
}

func initPanel(cfg *config.ServiceConfig) (apihttp.PanelOptions, error) {
	if cfg.AdminToken == "" {
		return apihttp.PanelOptions{}, nil
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") ||
		baseURL.Hostname() == "" || baseURL.User != nil || baseURL.ForceQuery ||
		baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return apihttp.PanelOptions{}, errors.New("base_url must be an absolute HTTP or HTTPS URL without credentials, query or fragment")
	}
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return apihttp.PanelOptions{}, errors.New("redis_url must be a valid Redis connection URL")
	}
	client := redis.NewClient(redisOptions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return apihttp.PanelOptions{}, errors.New("could not connect to Redis using redis_url")
	}
	return apihttp.PanelOptions{
		AdminToken:   cfg.AdminToken,
		Redis:        client,
		SecureCookie: baseURL.Scheme == "https",
	}, nil
}
