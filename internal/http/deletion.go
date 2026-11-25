package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/subscription"
)

type deletePage struct {
	EditorPath string
	Name       string
	Published  *subscription.Publication
	Error      string
}

func (p *adminPanel) confirmDelete(c *gin.Context) {
	p.renderDelete(c, http.StatusOK, "")
}

func (p *adminPanel) deleteSubscription(c *gin.Context) {
	if err := parseDraftForm(c); err != nil || c.Request.PostForm.Get("confirm") != "1" {
		p.renderDelete(c, http.StatusBadRequest, "请确认后再删除订阅。")
		return
	}
	err := p.store.Delete(c.Request.Context(), c.Param("id"))
	switch {
	case errors.Is(err, subscription.ErrNotFound):
		c.String(http.StatusNotFound, "找不到订阅。")
	case errors.Is(err, subscription.ErrConflict):
		p.renderDelete(c, http.StatusConflict, "订阅正在更新，请重新确认删除。")
	case err != nil:
		p.renderDelete(c, http.StatusServiceUnavailable, "暂时无法删除订阅，请重试。")
	default:
		c.Redirect(http.StatusSeeOther, "/admin")
	}
}

func (p *adminPanel) renderDelete(c *gin.Context, status int, message string) {
	sub, err := p.store.Get(c.Request.Context(), c.Param("id"))
	switch {
	case errors.Is(err, subscription.ErrNotFound):
		c.String(http.StatusNotFound, "找不到订阅。")
	case err != nil:
		c.String(http.StatusServiceUnavailable, "暂时无法读取订阅，请重试。")
	default:
		renderPanel(c, status, "admin_delete.html", deletePage{
			EditorPath: subscriptionPath(sub.ID),
			Name:       sub.Draft.Name,
			Published:  sub.Publication,
			Error:      message,
		})
	}
}
