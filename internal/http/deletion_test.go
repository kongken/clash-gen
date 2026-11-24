package http

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDeleteRequiresAdminConfirmation(t *testing.T) {
	router, cookie, editor := newDraftEditor(t, "待删除草稿")
	deletePath := editor + "/delete"
	confirmed := url.Values{"confirm": {"1"}}

	page := authRequest(router, http.MethodGet, editor, nil, cookie)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `href="`+deletePath+`"`) {
		t.Fatalf("editor returned %d without a deletion link", page.Code)
	}
	if response := authRequest(router, http.MethodPost, deletePath, confirmed, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous deletion returned %d", response.Code)
	}
	page = authRequest(router, http.MethodGet, deletePath, nil, cookie)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "确认删除") ||
		!strings.Contains(page.Body.String(), `href="`+editor+`">取消<`) {
		t.Fatalf("confirmation page returned %d without confirm and cancel controls", page.Code)
	}
	if response := authRequest(router, http.MethodPost, deletePath, url.Values{"confirm": {"0"}}, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("deletion without confirmation returned %d", response.Code)
	}
	if response := authRequest(router, http.MethodGet, editor, nil, cookie); response.Code != http.StatusOK {
		t.Fatalf("visit after cancel returned %d", response.Code)
	}
	if response := authRequest(router, http.MethodPost, deletePath, confirmed, cookie); response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/admin" {
		t.Fatalf("confirmed deletion returned %d or did not go back to the list", response.Code)
	}
	if response := authRequest(router, http.MethodGet, editor, nil, cookie); response.Code != http.StatusNotFound {
		t.Fatalf("deleted draft remained editable: %d", response.Code)
	}
	list := authRequest(router, http.MethodGet, "/admin", nil, cookie)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "待删除草稿") {
		t.Fatalf("deleted draft remained in the subscription list: %d", list.Code)
	}
}

func TestDeleteRevokesLinksAndCleansUpAfterStorageRecovers(t *testing.T) {
	fixture := newPublicationFixture(t)
	originalConfig := fixture.s3Config
	proxy := newPublicationProxy(t, fixture.s3Config.Endpoint)
	fixture.s3Config.Endpoint = proxy.url

	router, publisher := fixture.router(t)
	cookie, editor := publicationEditor(t, router, "删除验收订阅")
	if response := authRequest(router, http.MethodPost, editor+"/publish", publicationForm("删除验收订阅", "已发布节点", "1"), cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("publish returned %d", response.Code)
	}
	_, otherEditor := publicationEditor(t, router, "保留验收订阅")
	if response := authRequest(router, http.MethodPost, otherEditor+"/publish", publicationForm("保留验收订阅", "保留节点", "1"), cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("publish other subscription returned %d", response.Code)
	}
	waitForPublicationObjects(t, originalConfig, 2)

	link := readPublishedLink(t, authRequest(router, http.MethodGet, editor, nil, cookie).Body.String())
	otherLink := readPublishedLink(t, authRequest(router, http.MethodGet, otherEditor, nil, cookie).Body.String())
	deletePath := editor + "/delete"
	if page := authRequest(router, http.MethodGet, deletePath, nil, cookie); page.Code != http.StatusOK {
		t.Fatalf("opening confirmation returned %d", page.Code)
	}
	if response := authRequest(router, http.MethodGet, editor, nil, cookie); response.Code != http.StatusOK {
		t.Fatalf("cancel left the subscription unreachable: %d", response.Code)
	}
	if response := authRequest(router, http.MethodGet, link.RequestURI(), nil, nil); response.Code != http.StatusOK {
		t.Fatalf("opening and cancelling confirmation revoked the link: %d", response.Code)
	}

	upload := proxy.gateNext(http.MethodPut)
	defer upload.release()
	pending := publishAsync(router, cookie, editor, publicationForm("删除验收订阅", "上传中的节点", "2"))
	upload.wait(t)
	proxy.unavailable.Store(true)
	stopCleanup := runCleanupForTest(t, publisher)
	defer stopCleanup()

	if response := authRequest(router, http.MethodPost, deletePath, url.Values{"confirm": {"1"}}, cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("deletion while MinIO was unavailable returned %d", response.Code)
	}
	if response := authRequest(router, http.MethodGet, link.RequestURI(), nil, nil); response.Code != http.StatusNotFound {
		t.Fatalf("old link was not revoked while MinIO was unavailable: %d", response.Code)
	}
	select {
	case <-proxy.deniedDelete:
	case <-time.After(10 * time.Second):
		t.Fatal("cleanup did not retry against unavailable object storage")
	}

	// The request entered the fault proxy before the outage and completes
	// its PUT after the subscription was deleted.
	upload.release()
	if response := waitForPublication(t, pending); response.Code != http.StatusNotFound {
		t.Fatalf("publication completing after deletion returned %d", response.Code)
	}
	waitForPublicationObjects(t, originalConfig, 3)
	stopCleanup()

	restarted, restartedPublisher := fixture.router(t)
	proxy.unavailable.Store(false)
	stopRestartedCleanup := runCleanupForTest(t, restartedPublisher)
	defer stopRestartedCleanup()
	waitForPublicationObjects(t, originalConfig, 1)

	if response := authRequest(restarted, http.MethodGet, link.RequestURI(), nil, nil); response.Code != http.StatusNotFound {
		t.Fatalf("link became reachable after MinIO recovery and service restart: %d", response.Code)
	}
	if response := authRequest(restarted, http.MethodGet, editor, nil, cookie); response.Code != http.StatusNotFound {
		t.Fatalf("publication or restart recreated the deleted subscription: %d", response.Code)
	}
	if response := authRequest(restarted, http.MethodGet, otherLink.RequestURI(), nil, nil); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "保留节点") {
		t.Fatalf("cleanup interfered with another subscription: %d", response.Code)
	}
}
