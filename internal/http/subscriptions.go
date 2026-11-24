package http

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/subscription"
)

//go:embed templates/admin_*.html
var adminHTML embed.FS

var adminTemplates = template.Must(template.ParseFS(adminHTML, "templates/admin_*.html"))

type adminPanel struct {
	store *subscription.Store
}

type listPage struct {
	Subscriptions []subscription.Subscription
	Name          string
	Error         string
}

type editPage struct {
	Subscription subscription.Subscription
	Error        string
	Notice       string
}

func registerSubscriptions(admin *gin.RouterGroup, store *subscription.Store) {
	panel := &adminPanel{store: store}
	admin.GET("", panel.list)
	admin.POST("/subscriptions", panel.create)
	admin.GET("/subscriptions/:id", panel.edit)
	admin.POST("/subscriptions/:id", panel.save)
}

func (p *adminPanel) list(c *gin.Context) {
	p.renderList(c, http.StatusOK, "", "")
}

func (p *adminPanel) renderList(c *gin.Context, status int, name, message string) {
	subscriptions, err := p.store.List(c.Request.Context())
	if err != nil {
		status = http.StatusServiceUnavailable
		message = "暂时无法读取订阅，请重试。"
	}
	renderPanel(c, status, "admin_list.html", listPage{Subscriptions: subscriptions, Name: name, Error: message})
}

func (p *adminPanel) create(c *gin.Context) {
	if err := parseDraftForm(c); err != nil {
		p.renderList(c, http.StatusBadRequest, "", "创建请求无效。")
		return
	}
	name := c.Request.PostForm.Get("name")
	sub, err := p.store.Create(c.Request.Context(), name)
	switch {
	case errors.Is(err, subscription.ErrName):
		p.renderList(c, http.StatusUnprocessableEntity, name, "请输入 1 至 120 个字符的订阅名称。")
	case err != nil:
		p.renderList(c, http.StatusServiceUnavailable, name, "暂时无法创建订阅，请重试。")
	default:
		c.Redirect(http.StatusSeeOther, subscriptionPath(sub.ID))
	}
}

func (p *adminPanel) edit(c *gin.Context) {
	var notice string
	if c.Query("saved") == "1" {
		notice = "草稿已保存。"
	}
	p.renderEdit(c, http.StatusOK, "", notice)
}

func (p *adminPanel) save(c *gin.Context) {
	if err := parseDraftForm(c); err != nil {
		p.renderEdit(c, http.StatusBadRequest, "保存请求无效。", "")
		return
	}
	version, err := strconv.ParseUint(c.Request.PostForm.Get("version"), 10, 64)
	if err != nil || version == 0 {
		p.renderEdit(c, http.StatusBadRequest, "草稿版本无效，请刷新后重试。", "")
		return
	}
	id := c.Param("id")
	draft := subscription.Draft{Name: c.Request.PostForm.Get("name")}
	err = p.store.SaveDraft(c.Request.Context(), id, version, draft)
	switch {
	case errors.Is(err, subscription.ErrName):
		p.renderEdit(c, http.StatusUnprocessableEntity, "请输入 1 至 120 个字符的订阅名称。", "")
	case errors.Is(err, subscription.ErrConflict):
		p.renderEdit(c, http.StatusConflict, "草稿已在其他请求中修改。已显示最新版本，请重新编辑。", "")
	case err != nil:
		p.renderEdit(c, http.StatusServiceUnavailable, "暂时无法保存草稿，请重试。", "")
	default:
		c.Redirect(http.StatusSeeOther, subscriptionPath(id)+"?saved=1")
	}
}

func (p *adminPanel) renderEdit(c *gin.Context, status int, message, notice string) {
	sub, err := p.store.Get(c.Request.Context(), c.Param("id"))
	switch {
	case errors.Is(err, subscription.ErrNotFound):
		c.String(http.StatusNotFound, "找不到订阅。")
	case err != nil:
		c.String(http.StatusServiceUnavailable, "暂时无法读取草稿，请重试。")
	default:
		renderPanel(c, status, "admin_edit.html", editPage{Subscription: sub, Error: message, Notice: notice})
	}
}

func parseDraftForm(c *gin.Context) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	return c.Request.ParseForm()
}

func renderPanel(c *gin.Context, status int, name string, data any) {
	var body bytes.Buffer
	if err := adminTemplates.ExecuteTemplate(&body, name, data); err != nil {
		c.String(http.StatusInternalServerError, "暂时无法显示页面。")
		return
	}
	c.Data(status, "text/html; charset=utf-8", body.Bytes())
}

func subscriptionPath(id string) string { return "/admin/subscriptions/" + id }
