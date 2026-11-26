package http

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResetPublishedLinkPreservesDownloadAcrossRestart(t *testing.T) {
	fixture := newPublicationFixture(t)
	router, _ := fixture.router(t)
	cookie, editor := publicationEditor(t, router, "重设链接测试")
	resetPath := editor + "/reset-link"

	if response := authRequest(router, http.MethodPost, resetPath, nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous link reset returned %d", response.Code)
	}
	if response := authRequest(router, http.MethodPost, resetPath, nil, cookie); response.Code != http.StatusConflict {
		t.Fatalf("reset for an unpublished subscription returned %d", response.Code)
	}

	if response := authRequest(router, http.MethodPost, editor+"/publish", publicationForm("重设链接测试", "已发布节点", "1"), cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("first publication returned %d", response.Code)
	}
	_, otherEditor := publicationEditor(t, router, "其他订阅")
	if response := authRequest(router, http.MethodPost, otherEditor+"/publish", publicationForm("其他订阅", "其他节点", "1"), cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("second publication returned %d", response.Code)
	}
	otherPage := authRequest(router, http.MethodGet, otherEditor, nil, cookie)
	otherLink := readPublishedLink(t, otherPage.Body.String())
	otherDownload := authRequest(router, http.MethodGet, otherLink.RequestURI(), nil, nil)
	if otherDownload.Code != http.StatusOK {
		t.Fatalf("other subscription download returned %d", otherDownload.Code)
	}

	if response := authRequest(router, http.MethodPost, editor, publicationForm("重设链接测试", "尚未发布的节点", "2"), cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("save unpublished draft returned %d", response.Code)
	}
	before := authRequest(router, http.MethodGet, editor, nil, cookie)
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), `action="`+resetPath+`"`) {
		t.Fatalf("published editor returned %d without a link reset form", before.Code)
	}
	oldLink := readPublishedLink(t, before.Body.String())
	publishedAt := renderedPublicationTime(t, before.Body.String())
	oldDownload := authRequest(router, http.MethodGet, oldLink.RequestURI(), nil, nil)
	if oldDownload.Code != http.StatusOK || !strings.Contains(oldDownload.Body.String(), "已发布节点") {
		t.Fatalf("original link returned %d or the wrong YAML", oldDownload.Code)
	}

	response := authRequest(router, http.MethodPost, resetPath, nil, cookie)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != editor+"?link-reset=1" {
		t.Fatalf("authenticated reset returned %d or redirected to the wrong editor", response.Code)
	}
	after := authRequest(router, http.MethodGet, response.Header().Get("Location"), nil, cookie)
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), "订阅链接已重设") ||
		!strings.Contains(after.Body.String(), `id="copy-download-link"`) || !strings.Contains(after.Body.String(), "有未发布变更") {
		t.Fatalf("editor after reset returned %d without reset feedback, the copy action, or the unpublished change", after.Code)
	}
	if got := renderedPublicationTime(t, after.Body.String()); got != publishedAt {
		t.Errorf("published time changed after reset: %s → %s", publishedAt, got)
	}
	newLink := readPublishedLink(t, after.Body.String())
	if newLink.Host != oldLink.Host || newLink.Path != oldLink.Path || newLink.Query().Get("token") == oldLink.Query().Get("token") {
		t.Fatal("reset must rotate the token while preserving the URL path and host")
	}

	restarted, _ := fixture.router(t)
	for _, requestRouter := range []*gin.Engine{router, restarted} {
		for _, download := range []struct {
			name, path, body string
			status           int
		}{
			{"old link", oldLink.RequestURI(), "找不到订阅。", http.StatusNotFound},
			{"new link", newLink.RequestURI(), oldDownload.Body.String(), http.StatusOK},
			{"other subscription", otherLink.RequestURI(), otherDownload.Body.String(), http.StatusOK},
		} {
			recorder := authRequest(requestRouter, http.MethodGet, download.path, nil, nil)
			if recorder.Code != download.status || recorder.Body.String() != download.body {
				t.Errorf("%s returned %d or unexpected content, want status %d", download.name, recorder.Code, download.status)
			}
		}
	}
}

func renderedPublicationTime(t *testing.T, page string) string {
	t.Helper()
	_, timestamp, found := strings.Cut(page, `<time datetime="`)
	if !found {
		t.Fatal("published time is missing")
	}
	timestamp, _, found = strings.Cut(timestamp, `"`)
	if !found || timestamp == "" {
		t.Fatal("published time is incomplete")
	}
	return timestamp
}
