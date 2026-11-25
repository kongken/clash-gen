package http

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/subscription"
)

func (p *adminPanel) publish(c *gin.Context) {
	version, draft, err := readDraftPost(c)
	if err != nil {
		p.renderEdit(c, http.StatusBadRequest, err.Error(), "")
		return
	}
	id := c.Param("id")
	err = p.publisher.Publish(c.Request.Context(), id, version, draft)
	page := editPage{Version: version, Draft: draft}
	var invalid subscription.DraftValidationError
	switch {
	case errors.Is(err, subscription.ErrName):
		page.Error = "请输入 1 至 120 个字符的订阅名称。"
		p.renderEditPage(c, http.StatusUnprocessableEntity, page)
	case errors.As(err, &invalid):
		page.setValidationError(invalid)
		p.renderEditPage(c, http.StatusUnprocessableEntity, page)
	case errors.Is(err, subscription.ErrNotFound):
		c.String(http.StatusNotFound, "找不到订阅。")
	case errors.Is(err, subscription.ErrConflict):
		p.renderEdit(c, http.StatusConflict, "草稿或发布状态已更新。请检查最新草稿后重新发布。", "")
	case errors.Is(err, subscription.ErrUploadFailed):
		p.renderEdit(c, http.StatusBadGateway, "上传失败，请重试。", "")
	case err != nil:
		p.renderEdit(c, http.StatusServiceUnavailable, "暂时无法完成发布，请重试。", "")
	default:
		c.Redirect(http.StatusSeeOther, subscriptionPath(id)+"?published=1")
	}
}

const downloadTokenContextKey = "clash-gen:download-token"

func scrubDownloadToken(c *gin.Context) {
	request := c.Request
	if request.URL.Path == "/subscription" || strings.HasPrefix(request.URL.Path, "/subscription/") {
		query := request.URL.Query()
		if query.Has("token") {
			c.Set(downloadTokenContextKey, query.Get("token"))
			query.Del("token")
			request.URL.RawQuery = query.Encode()
			request.URL.ForceQuery = false
			request.RequestURI = request.URL.RequestURI()
		}
	}
	c.Next()
}

func (p *adminPanel) download(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), subscription.ObjectTimeout)
	defer cancel()
	body, length, err := p.publisher.Download(ctx, c.Param("id"), c.GetString(downloadTokenContextKey))
	switch {
	case errors.Is(err, subscription.ErrNotFound):
		c.String(http.StatusNotFound, "找不到订阅。")
	case err != nil:
		c.String(http.StatusServiceUnavailable, "暂时无法下载订阅，请重试。")
	default:
		defer body.Close()
		c.Header("X-Content-Type-Options", "nosniff")
		c.DataFromReader(http.StatusOK, length, subscription.YAMLContentType, body, nil)
	}
}

func (p *adminPanel) resetLink(c *gin.Context) {
	id := c.Param("id")
	err := p.store.ResetDownloadToken(c.Request.Context(), id)
	switch {
	case errors.Is(err, subscription.ErrNotFound):
		c.String(http.StatusNotFound, "找不到订阅。")
	case errors.Is(err, subscription.ErrNotPublished):
		p.renderEdit(c, http.StatusConflict, "请先发布订阅，再重设链接。", "")
	case errors.Is(err, subscription.ErrConflict):
		p.renderEdit(c, http.StatusConflict, "订阅状态已更新，请重试。", "")
	case err != nil:
		p.renderEdit(c, http.StatusServiceUnavailable, "暂时无法重设订阅链接，请重试。", "")
	default:
		c.Redirect(http.StatusSeeOther, subscriptionPath(id)+"?link-reset=1")
	}
}

func downloadLink(baseURL *url.URL, sub subscription.Subscription) string {
	if sub.Publication == nil || sub.DownloadToken == "" || baseURL == nil {
		return ""
	}
	link := baseURL.JoinPath("subscription", sub.ID)
	link.RawQuery = url.Values{"token": {sub.DownloadToken}}.Encode()
	return link.String()
}
