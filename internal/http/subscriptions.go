package http

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/subscription"
)

//go:embed templates/admin_*.html
var adminHTML embed.FS

var adminTemplates = template.Must(template.New("admin").Funcs(template.FuncMap{
	"shadowsocksCiphers": subscription.ShadowsocksCiphers,
	"formatPublishedAt":  func(at time.Time) string { return at.UTC().Format("2006-01-02 15:04:05") },
	"vmessCiphers":       subscription.VMessCiphers,
	"lines":              func(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") },
	"add":                func(a, b int) int { return a + b },
	"emptyNode":          func() subscription.Node { return subscription.Node{Type: "ss"} },
}).ParseFS(adminHTML, "templates/admin_*.html"))

type adminPanel struct {
	store     *subscription.Store
	publisher *subscription.Publisher
	baseURL   *url.URL
}

type listPage struct {
	Subscriptions []subscription.Subscription
	Name          string
	Error         string
}

type editPage struct {
	Subscription subscription.Subscription
	Draft        subscription.Draft
	Version      uint64
	Preview      string
	Error        string
	Notice       string
	Conflict     bool
	UploadFailed bool
	CanPublish   bool
	DownloadLink string
}

func registerSubscriptions(r *gin.Engine, admin *gin.RouterGroup, options PanelOptions) {
	panel := &adminPanel{
		store:     subscription.NewStore(options.Redis),
		publisher: options.Publisher,
		baseURL:   options.BaseURL,
	}
	admin.GET("", panel.list)
	admin.POST("/subscriptions", panel.create)
	admin.GET("/subscriptions/:id", panel.edit)
	admin.POST("/subscriptions/:id", panel.save)
	admin.POST("/subscriptions/:id/preview", panel.preview)
	admin.GET("/subscriptions/:id/delete", panel.confirmDelete)
	admin.POST("/subscriptions/:id/delete", panel.deleteSubscription)
	if panel.canPublish() {
		admin.POST("/subscriptions/:id/publish", panel.publish)
		admin.POST("/subscriptions/:id/reset-link", panel.resetLink)
		r.GET("/subscription/:id", sensitiveRequest, panel.download)
		// Gin's automatic slash redirect logs the URI before middleware.
		r.GET("/subscription/:id/", sensitiveRequest, panel.download)
	}
}

func (p *adminPanel) canPublish() bool { return p.publisher != nil && p.baseURL != nil }

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
	switch {
	case c.Query("saved") == "1":
		notice = "草稿已保存。"
	case c.Query("published") == "1":
		notice = "订阅已发布。"
	case c.Query("link-reset") == "1":
		notice = "订阅链接已重设，旧链接已失效。"
	}
	p.renderEdit(c, http.StatusOK, "", notice)
}

func (p *adminPanel) save(c *gin.Context) {
	version, draft, err := readDraftPost(c)
	if err != nil {
		p.renderEdit(c, http.StatusBadRequest, err.Error(), "")
		return
	}
	id := c.Param("id")
	err = p.store.SaveDraft(c.Request.Context(), id, version, draft)
	switch {
	case errors.Is(err, subscription.ErrName):
		p.renderEditPage(c, http.StatusUnprocessableEntity, editPage{Version: version, Draft: draft, Error: "请输入 1 至 120 个字符的订阅名称。"})
	case errors.Is(err, subscription.ErrConflict):
		p.renderEdit(c, http.StatusConflict, "草稿已在其他请求中修改。已显示最新版本，请重新编辑。", "")
	case err != nil:
		p.renderEditPage(c, http.StatusServiceUnavailable, editPage{Version: version, Draft: draft, Error: "暂时无法保存草稿，请重试。"})
	default:
		c.Redirect(http.StatusSeeOther, subscriptionPath(id)+"?saved=1")
	}
}

func (p *adminPanel) preview(c *gin.Context) {
	version, draft, err := readDraftPost(c)
	if err != nil {
		p.renderEdit(c, http.StatusBadRequest, err.Error(), "")
		return
	}
	page := editPage{Draft: draft, Version: version}
	output, err := subscription.GenerateYAML(draft)
	if err != nil {
		page.Error = err.Error()
		p.renderEditPage(c, http.StatusUnprocessableEntity, page)
		return
	}
	page.Preview = string(output)
	p.renderEditPage(c, http.StatusOK, page)
}

func (p *adminPanel) renderEdit(c *gin.Context, status int, message, notice string) {
	p.renderEditPage(c, status, editPage{Error: message, Notice: notice})
}

func (p *adminPanel) renderEditPage(c *gin.Context, status int, page editPage) {
	sub, err := p.store.Get(c.Request.Context(), c.Param("id"))
	switch {
	case errors.Is(err, subscription.ErrNotFound):
		c.String(http.StatusNotFound, "找不到订阅。")
	case err != nil:
		c.String(http.StatusServiceUnavailable, "暂时无法读取草稿，请重试。")
	default:
		page.Subscription = sub
		page.CanPublish = p.canPublish()
		page.DownloadLink = downloadLink(p.baseURL, sub)
		page.Conflict = status == http.StatusConflict
		page.UploadFailed = status == http.StatusBadGateway
		if page.Version == 0 {
			page.Draft = sub.Draft
			page.Version = sub.DraftVersion
		}
		renderPanel(c, status, "admin_edit.html", page)
	}
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
