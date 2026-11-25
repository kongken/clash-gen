package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/kongken/clash-gen/internal/config"
	"github.com/kongken/clash-gen/internal/subscription"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/html"
)

func TestPublishPreviewMatchesAnonymousDownload(t *testing.T) {
	fixture := newPublicationFixture(t)
	var observedURI, observedURL string
	router, _ := fixture.router(t, func(c *gin.Context) {
		observedURI = c.Request.RequestURI
		observedURL = c.Request.URL.String()
		c.Next()
	})
	cookie, editor := publicationEditor(t, router, "家庭订阅")

	unpublished := authRequest(router, http.MethodGet, editor, nil, cookie)
	if unpublished.Code != http.StatusOK || !strings.Contains(unpublished.Body.String(), ">未发布<") {
		t.Fatalf("new editor returned %d without unpublished status", unpublished.Code)
	}
	publicPath := strings.Replace(editor, "/admin/subscriptions/", "/subscription/", 1)
	if response := authRequest(router, http.MethodGet, publicPath+"?token=fictional", nil, nil); response.Code != http.StatusNotFound {
		t.Fatalf("unpublished subscription returned %d", response.Code)
	}
	invalidForm := url.Values{"name": {"家庭订阅"}, "version": {"1"}}
	if response := authRequest(router, http.MethodPost, editor+"/publish", invalidForm, cookie); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "至少添加一个有效节点") {
		t.Fatalf("publish without nodes returned %d", response.Code)
	}

	form := mixedProtocolForm("家庭订阅", "1")
	if anonymous := authRequest(router, http.MethodPost, editor+"/publish", form, nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous publish returned %d", anonymous.Code)
	}
	preview := authRequest(router, http.MethodPost, editor+"/preview", form, cookie)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview returned %d", preview.Code)
	}
	want := yamlFromPreview(t, preview.Body.String())
	if response := authRequest(router, http.MethodPost, editor+"/publish", form, cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("publish returned %d %q", response.Code, response.Body.String())
	}
	published := authRequest(router, http.MethodGet, editor, nil, cookie)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), ">已发布<") || !strings.Contains(published.Body.String(), " UTC</time>") {
		t.Fatalf("published editor returned %d without state or timestamp", published.Code)
	}
	link := readPublishedLink(t, published.Body.String())
	if link.Scheme != "https" || link.Host != fixture.baseURL.Host || link.Path != publicPath {
		t.Fatalf("link does not use configured public address")
	}

	download := authRequest(router, http.MethodGet, link.RequestURI(), nil, nil)
	if download.Code != http.StatusOK || download.Header().Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(download.Header().Get("Content-Type"), "application/yaml") || !bytes.Equal(download.Body.Bytes(), want) {
		t.Fatalf("anonymous download returned %d or differed from preview", download.Code)
	}
	if observedURI != link.Path || observedURL != link.Path {
		t.Fatal("download token reached request observers")
	}

	trailing := *link
	trailing.Path += "/"
	slashDownload := authRequest(router, http.MethodGet, trailing.RequestURI(), nil, nil)
	if slashDownload.Code != http.StatusOK || !bytes.Equal(slashDownload.Body.Bytes(), want) {
		t.Fatalf("anonymous download with trailing slash returned %d or differed from preview", slashDownload.Code)
	}
	if observedURI != trailing.Path || observedURL != trailing.Path {
		t.Fatal("download token reached request observers on trailing-slash path")
	}

	unmatched := *link
	unmatched.Path += "/unmatched"
	if response := authRequest(router, http.MethodGet, unmatched.RequestURI(), nil, nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown download path returned %d", response.Code)
	}
	if observedURI != unmatched.Path || observedURL != unmatched.Path {
		t.Fatal("download token reached request observers on unmatched path")
	}

	for _, target := range []string{link.Path, link.Path + "?token=wrong"} {
		if response := authRequest(router, http.MethodGet, target, nil, nil); response.Code != http.StatusNotFound || bytes.Equal(want, response.Body.Bytes()) {
			t.Fatalf("unauthorized download returned %d", response.Code)
		}
	}
	verifyMihomoYAML(t, download.Body.Bytes())
}

func TestPublicationSurvivesStorageFailuresAndConcurrentEdits(t *testing.T) {
	fixture := newPublicationFixture(t)
	originalConfig := fixture.s3Config
	proxy := newPublicationProxy(t, fixture.s3Config.Endpoint)
	fixture.s3Config.Endpoint = proxy.url

	router, publisher := fixture.router(t)
	cookie, editor := publicationEditor(t, router, "发布测试")
	formA := publicationForm("发布测试", "首发节点", "1")
	if response := authRequest(router, http.MethodPost, editor+"/publish", formA, cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("publish first draft returned %d", response.Code)
	}
	page := authRequest(router, http.MethodGet, editor, nil, cookie)
	link := readPublishedLink(t, page.Body.String())
	first := authRequest(router, http.MethodGet, link.RequestURI(), nil, nil)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "首发节点") {
		t.Fatalf("first download returned %d", first.Code)
	}
	assertCurrent := func(r *gin.Engine, want []byte) {
		t.Helper()
		got := authRequest(r, http.MethodGet, link.RequestURI(), nil, nil)
		if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), want) {
			t.Fatalf("unchanged download URL returned %d or unexpected YAML", got.Code)
		}
	}

	formB := publicationForm("发布测试", "旧草稿节点", "2")
	if response := authRequest(router, http.MethodPost, editor, formB, cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("save second draft returned %d", response.Code)
	}
	dirty := authRequest(router, http.MethodGet, editor, nil, cookie)
	if !strings.Contains(dirty.Body.String(), ">有未发布变更<") || readPublishedLink(t, dirty.Body.String()).String() != link.String() {
		t.Fatal("saving draft did not show unpublished changes and the original link")
	}
	formB.Set("version", "3")

	proxy.denyPut.Store(true)
	failure := authRequest(router, http.MethodPost, editor+"/publish", formB, cookie)
	if failure.Code != http.StatusBadGateway || !strings.Contains(failure.Body.String(), "上传失败") {
		t.Fatalf("failed upload returned %d", failure.Code)
	}
	assertCurrent(router, first.Body.Bytes())
	proxy.denyPut.Store(false)

	proxy.denyDelete.Store(true)
	stopCleanup := runCleanupForTest(t, publisher)
	defer stopCleanup()

	staleGate := proxy.gateNext(http.MethodPut)
	defer staleGate.release()
	stale := publishAsync(router, cookie, editor, formB)
	staleGate.wait(t)
	formC := publicationForm("发布测试", "新草稿节点", "3")
	if response := authRequest(router, http.MethodPost, editor, formC, cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("save while previous draft uploads returned %d", response.Code)
	}
	staleGate.release()
	if response := waitForPublication(t, stale); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "重新发布") {
		t.Fatalf("stale draft returned %d", response.Code)
	}
	assertCurrent(router, first.Body.Bytes())

	formC.Set("version", "4")
	earlierGate := proxy.gateNext(http.MethodPut)
	defer earlierGate.release()
	earlier := publishAsync(router, cookie, editor, formC)
	earlierGate.wait(t)

	downloadGate := proxy.gateNext(http.MethodGet)
	defer downloadGate.release()
	startedDownload := make(chan *httptest.ResponseRecorder, 1)
	go func() { startedDownload <- authRequest(router, http.MethodGet, link.RequestURI(), nil, nil) }()
	downloadGate.wait(t)

	if response := authRequest(router, http.MethodPost, editor+"/publish", formC, cookie); response.Code != http.StatusSeeOther {
		t.Fatalf("later publish returned %d", response.Code)
	}
	earlierGate.release()
	if response := waitForPublication(t, earlier); response.Code != http.StatusConflict {
		t.Fatalf("older simultaneous publish returned %d", response.Code)
	}
	select {
	case <-proxy.deniedDelete:
	case <-time.After(10 * time.Second):
		t.Fatal("cleanup worker did not attempt deletion during the MinIO failure")
	}
	stopCleanup()

	restarted, restartedPublisher := fixture.router(t)
	proxy.denyDelete.Store(false)
	stopRestartedCleanup := runCleanupForTest(t, restartedPublisher)
	defer stopRestartedCleanup()
	current := authRequest(restarted, http.MethodGet, link.RequestURI(), nil, nil)
	if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), "新草稿节点") {
		t.Fatalf("restart changed the active publication: %d", current.Code)
	}
	reopened := authRequest(restarted, http.MethodGet, editor, nil, cookie)
	if readPublishedLink(t, reopened.Body.String()).String() != link.String() {
		t.Fatal("restart changed the subscription link")
	}

	// Leave the GET at the MinIO boundary until the cleanup worker can poll
	// at least once with deletion enabled.
	time.Sleep(2 * time.Second)
	downloadGate.release()
	if response := waitForPublication(t, startedDownload); response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), first.Body.Bytes()) {
		t.Fatalf("download started before publication did not finish with the previous YAML: %d", response.Code)
	}

	waitForPublicationObjects(t, originalConfig, 1)
	assertCurrent(restarted, current.Body.Bytes())
}

type publicationProxy struct {
	url          string
	unavailable  atomic.Bool
	denyPut      atomic.Bool
	denyDelete   atomic.Bool
	deniedDelete chan struct{}
	mu           sync.Mutex
	next         *publicationGate
}

func newPublicationProxy(t *testing.T, endpoint string) *publicationProxy {
	t.Helper()
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal("invalid acceptance S3 endpoint")
	}
	upstream := httputil.NewSingleHostReverseProxy(target)
	proxy := &publicationProxy{deniedDelete: make(chan struct{}, 1)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if proxy.unavailable.Load() || (r.Method == http.MethodPut && proxy.denyPut.Load()) || (r.Method == http.MethodDelete && proxy.denyDelete.Load()) {
			if r.Method == http.MethodDelete {
				select {
				case proxy.deniedDelete <- struct{}{}:
				default:
				}
			}
			w.WriteHeader(http.StatusForbidden)
			return
		}

		proxy.mu.Lock()
		gate := proxy.next
		if gate != nil && gate.method == r.Method {
			proxy.next = nil
		} else {
			gate = nil
		}
		proxy.mu.Unlock()
		if gate != nil {
			close(gate.entered)
			select {
			case <-gate.resume:
			case <-r.Context().Done():
				return
			}
		}
		// The signed Host remains the same while the request is routed to MinIO.
		upstream.ServeHTTP(w, r)
	}))
	proxy.url = server.URL
	t.Cleanup(server.Close)
	return proxy
}

type publicationGate struct {
	method  string
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (p *publicationProxy) gateNext(method string) *publicationGate {
	gate := &publicationGate{method: method, entered: make(chan struct{}), resume: make(chan struct{})}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next = gate
	return gate
}

func (g *publicationGate) wait(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for S3 %s", g.method)
	}
}

func (g *publicationGate) release() { g.once.Do(func() { close(g.resume) }) }

func publishAsync(router *gin.Engine, cookie *http.Cookie, editor string, form url.Values) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- authRequest(router, http.MethodPost, editor+"/publish", form, cookie) }()
	return done
}

func waitForPublication(t *testing.T, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-done:
		return response
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for HTTP response")
		return nil
	}
}

func runCleanupForTest(t *testing.T, publisher *subscription.Publisher) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		publisher.RunCleanup(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

func publicationS3Client(cfg config.ObjectStoreConfig) *s3.Client {
	return s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
	})
}

func waitForPublicationObjects(t *testing.T, cfg config.ObjectStoreConfig, want int) {
	t.Helper()
	client := publicationS3Client(cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	lastCount := -1
	for {
		result, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(cfg.Bucket), Prefix: aws.String(cfg.Prefix + "/"),
		})
		if err != nil {
			t.Fatalf("could not list publication test prefix: %v", err)
		}
		lastCount = len(result.Contents)
		if lastCount == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("object cleanup left %d objects, want %d", lastCount, want)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

type publicationFixture struct {
	redisOptions *redis.Options
	s3Config     config.ObjectStoreConfig
	baseURL      *url.URL
}

func newPublicationFixture(t *testing.T) publicationFixture {
	t.Helper()
	redisURL := os.Getenv("CLASH_GEN_TEST_REDIS_URL")
	s3Endpoint := os.Getenv("CLASH_GEN_TEST_S3_ENDPOINT")
	bucket := os.Getenv("CLASH_GEN_TEST_S3_BUCKET")
	accessKeyID := os.Getenv("CLASH_GEN_TEST_S3_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("CLASH_GEN_TEST_S3_SECRET_ACCESS_KEY")
	if redisURL == "" && s3Endpoint == "" && bucket == "" && accessKeyID == "" && secretAccessKey == "" {
		t.Skip("set CLASH_GEN_TEST_REDIS_URL and CLASH_GEN_TEST_S3_* to run real-store acceptance")
	}
	if redisURL == "" || s3Endpoint == "" || bucket == "" || accessKeyID == "" || secretAccessKey == "" {
		t.Fatal("real-store acceptance requires Redis URL, S3 endpoint, bucket, access key, and secret key")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal("invalid CLASH_GEN_TEST_REDIS_URL")
	}
	return publicationFixture{
		redisOptions: redisOptions,
		s3Config: config.ObjectStoreConfig{
			Endpoint: s3Endpoint, Bucket: bucket, Region: "us-east-1", Prefix: "test/" + rand.Text(),
			AccessKeyID: accessKeyID, SecretAccessKey: secretAccessKey,
		},
		baseURL: &url.URL{Scheme: "https", Host: "clash.example.test"},
	}
}

func (f publicationFixture) router(t *testing.T, middleware ...gin.HandlerFunc) (*gin.Engine, *subscription.Publisher) {
	t.Helper()
	redisClient := redis.NewClient(f.redisOptions)
	t.Cleanup(func() { _ = redisClient.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatal("could not connect to the Redis acceptance instance")
	}
	objects, err := subscription.NewS3Objects(f.s3Config)
	if err != nil {
		t.Fatal(err)
	}
	publisher := subscription.NewPublisher(subscription.NewStore(redisClient), objects)
	router := gin.New()
	router.Use(middleware...)
	if err := RegisterRoutes(router, PanelOptions{AdminToken: "test-admin-token", Redis: redisClient, BaseURL: f.baseURL, Publisher: publisher}); err != nil {
		t.Fatal(err)
	}
	return router, publisher
}

func publicationEditor(t *testing.T, router *gin.Engine, name string) (*http.Cookie, string) {
	t.Helper()
	login := authRequest(router, http.MethodPost, "/login", url.Values{"token": {"test-admin-token"}}, nil)
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login returned %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	created := authRequest(router, http.MethodPost, "/admin/subscriptions", url.Values{"name": {name}}, cookie)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create subscription returned %d", created.Code)
	}
	return cookie, created.Header().Get("Location")
}

func publicationForm(name, node, version string) url.Values {
	return url.Values{
		"name": {name}, "version": {version}, "node_type": {"ss"}, "node_name": {node},
		"node_server": {"ss.example.test"}, "node_port": {"443"},
		"node_cipher": {"aes-128-gcm"}, "node_password": {"fictional-password"},
	}
}

func readPublishedLink(t *testing.T, page string) *url.URL {
	t.Helper()
	tokens := html.NewTokenizer(strings.NewReader(page))
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			t.Fatal("download link is missing from the editor")
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		input := tokens.Token()
		if input.Data != "input" {
			continue
		}
		var id, value string
		for _, attribute := range input.Attr {
			switch attribute.Key {
			case "id":
				id = attribute.Val
			case "value":
				value = attribute.Val
			}
		}
		if id == "download-link" {
			link, err := url.Parse(value)
			if err != nil || link.Query().Get("token") == "" {
				t.Fatal("download link is invalid")
			}
			return link
		}
	}
}
