package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"routebox/backend/internal/mtproto"
)

// freeAddr returns a loopback address nothing is listening on right now.
func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	defer l.Close()

	return "127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// mtprotoWebBlock is the "web" object GET /api/mtproto carries.
type mtprotoWebBlock struct {
	Enabled      bool   `json:"enabled"`
	Managed      bool   `json:"managed"`
	Hostname     string `json:"hostname"`
	Listen       string `json:"listen"`
	PublicDir    string `json:"public_dir"`
	CanIssueLink bool   `json:"can_issue_link"`
	Sessions     int    `json:"sessions"`
	Streams      int    `json:"streams"`
}

func getWebBlock(t *testing.T, r http.Handler) mtprotoWebBlock {
	t.Helper()

	var body struct {
		Web *mtprotoWebBlock `json:"web"`
	}

	decodeMtprotoData(t, mtprotoReq(t, r, http.MethodGet, "/api/mtproto", ""), &body)

	if body.Web == nil {
		t.Fatal("the status answer has no web block")
	}

	return *body.Web
}

// answers reports whether something accepts TCP connections on addr.
func answers(addr string) bool {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return false
	}

	_ = c.Close()

	return true
}

func TestStatusCarriesTheWebBlock(t *testing.T) {
	_, r := newMtprotoTestHandler(t)

	web := getWebBlock(t, r)

	if web.Enabled || web.Managed || web.CanIssueLink {
		t.Errorf("web = %+v, want enabled/managed/can_issue_link all false by default", web)
	}

	if web.Listen != "127.0.0.1:9446" {
		t.Errorf("listen = %q, want the default 127.0.0.1:9446", web.Listen)
	}
}

func TestStatusWebHostnameFollowsBootstrap(t *testing.T) {
	h, r := newMtprotoTestHandler(t)

	if err := h.settings.SetBootstrap("/panel-secret", "127.0.0.1:9443", "", 443); err != nil {
		t.Fatal(err)
	}

	if err := h.settings.Update(map[string]any{
		"server.public_host":   "box.example.com",
		"mtproto.web_hostname": "other.example.com",
	}); err != nil {
		t.Fatal(err)
	}

	web := getWebBlock(t, r)

	// One external port, one name: the operator's own hostname cannot win on
	// a bootstrapped install, or it would disagree with the certificate.
	if web.Hostname != "box.example.com" {
		t.Errorf("hostname = %q, want the public host", web.Hostname)
	}

	if !web.Managed {
		t.Error("managed = false on a bootstrapped install")
	}
}

func TestUpdateWebEnabledStartsAndStopsTheFront(t *testing.T) {
	h, r := newMtprotoTestHandler(t)

	// Start from what a non-bootstrapped install with the feature off looks
	// like: no front at all.
	if err := h.webFront.Stop(); err != nil {
		t.Fatal(err)
	}

	addr := freeAddr(t)

	rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto",
		`{"web_enabled":true,"web_hostname":"proxy.example.com","web_listen":"`+addr+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: status = %d, body = %s", rec.Code, rec.Body)
	}

	if got := h.webFront.Listen(); got != addr {
		t.Errorf("front listen = %q, want %q after web_enabled=true", got, addr)
	}

	rec = mtprotoReq(t, r, http.MethodPut, "/api/mtproto", `{"web_enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: status = %d, body = %s", rec.Code, rec.Body)
	}

	if got := h.webFront.Listen(); got != "" {
		t.Errorf("front listen = %q, want it stopped after web_enabled=false", got)
	}
}

func TestUpdateWebListenRebindsTheFront(t *testing.T) {
	h, r := newMtprotoTestHandler(t)

	first := freeAddr(t)
	if rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto",
		`{"web_enabled":true,"web_hostname":"proxy.example.com","web_listen":"`+first+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("enable: status = %d, body = %s", rec.Code, rec.Body)
	}

	second := freeAddr(t)
	if rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto", `{"web_listen":"`+second+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("rebind: status = %d, body = %s", rec.Code, rec.Body)
	}

	if got := h.webFront.Listen(); got != second {
		t.Errorf("front listen = %q, want %q", got, second)
	}

	if !answers(second) {
		t.Error("nothing answers on the new address")
	}
}

func TestUpdateWebListenBusyPortFails(t *testing.T) {
	h, r := newMtprotoTestHandler(t)

	first := freeAddr(t)
	if rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto",
		`{"web_enabled":true,"web_hostname":"proxy.example.com","web_listen":"`+first+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("enable: status = %d, body = %s", rec.Code, rec.Body)
	}

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto",
		`{"web_listen":"`+busy.Addr().String()+`","web_hostname":"changed.example.com"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", rec.Code, rec.Body)
	}

	// The settings have to keep describing what is actually serving, all four
	// web keys included, not just the one that failed.
	got := h.settings.Get().Mtproto
	if got.WebListen != first || got.WebHostname != "proxy.example.com" || !got.WebEnabled {
		t.Errorf("settings = listen %q hostname %q enabled %v, want the previous values",
			got.WebListen, got.WebHostname, got.WebEnabled)
	}

	if h.webFront.Listen() != first || !answers(first) {
		t.Error("the old listener stopped serving after a failed rebind")
	}
}

// Off a bootstrapped install web_public_dir is the stub the front serves while
// no WEB handler is set; moving it must show at once, not after a restart.
func TestUpdateWebPublicDirSwapsTheFrontsStub(t *testing.T) {
	_, r := newMtprotoTestHandler(t)

	site := func(body string) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}

		return dir
	}
	first, second := site("first site"), site("second site")

	addr := freeAddr(t)
	if rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto",
		`{"web_enabled":true,"web_hostname":"proxy.example.com","web_listen":"`+addr+`","web_public_dir":"`+first+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("enable: status = %d, body = %s", rec.Code, rec.Body)
	}

	if got := frontBody(t, addr); got != "first site" {
		t.Fatalf("front serves %q, want the first site", got)
	}

	if rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto", `{"web_public_dir":"`+second+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("move the site: status = %d, body = %s", rec.Code, rec.Body)
	}

	if got := frontBody(t, addr); got != "second site" {
		t.Errorf("front serves %q after web_public_dir moved, want the second site", got)
	}
}

// frontBody fetches addr's root over a fresh connection.
func frontBody(t *testing.T, addr string) string {
	t.Helper()

	c := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 3 * time.Second}

	resp, err := c.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET %s: %v", addr, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestUpdateRejectsABadWebHostname(t *testing.T) {
	_, r := newMtprotoTestHandler(t)

	rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto", `{"web_hostname":"Not A Host:443"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body = %s", rec.Code, rec.Body)
	}
}

// webLinkInstall is a handler with one client and every setting a WEB link
// needs, minus the running proxy.
func webLinkInstall(t *testing.T, webEnabled bool) (*Handler, http.Handler) {
	t.Helper()

	h, r := newMtprotoTestHandler(t)
	if err := h.mtproto.Store().Put(mtproto.Client{Name: "alice", Secret: testSecret, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if err := h.settings.Update(map[string]any{
		"mtproto.masking_domain": "example.com",
		"mtproto.public_host":    "panel.example.com",
		"mtproto.listen":         "127.0.0.1:0",
		"mtproto.web_enabled":    webEnabled,
		"mtproto.web_hostname":   "proxy.example.com",
		"mtproto.web_listen":     h.webFront.Listen(),
	}); err != nil {
		t.Fatal(err)
	}

	if rec := mtprotoReq(t, r, http.MethodPost, "/api/mtproto/enable", ""); rec.Code != http.StatusOK {
		t.Fatalf("enable: status = %d, body = %s", rec.Code, rec.Body)
	}
	t.Cleanup(func() { _ = h.mtproto.Stop() })

	return h, r
}

func TestLinkCarriesWebProxyWhenIssuable(t *testing.T) {
	_, r := webLinkInstall(t, true)

	var body struct {
		TG       string `json:"tg"`
		WebProxy *struct {
			TG  string `json:"tg"`
			Web string `json:"web"`
		} `json:"webproxy"`
	}

	decodeMtprotoData(t, mtprotoReq(t, r, http.MethodGet, "/api/mtproto/clients/alice/link", ""), &body)

	if body.WebProxy == nil {
		t.Fatal("webproxy = null with the WEB proxy enabled and running")
	}

	if !strings.HasPrefix(body.WebProxy.TG, "tg://webproxy?server=proxy.example.com") {
		t.Errorf("webproxy.tg = %q", body.WebProxy.TG)
	}

	if body.WebProxy.Web == "" {
		t.Error("webproxy.web is empty")
	}

	if !strings.HasPrefix(body.TG, "tg://proxy?") {
		t.Errorf("tg = %q, want the Fake TLS link alongside", body.TG)
	}
}

func TestLinkWebProxyIsNullWhenOff(t *testing.T) {
	_, r := webLinkInstall(t, false)

	rec := mtprotoReq(t, r, http.MethodGet, "/api/mtproto/clients/alice/link", "")

	var body map[string]json.RawMessage

	decodeMtprotoData(t, rec, &body)

	if string(body["webproxy"]) != "null" {
		t.Errorf("webproxy = %s, want null", body["webproxy"])
	}

	if !strings.Contains(string(body["tg"]), "tg://proxy?") {
		t.Errorf("tg = %s, want the Fake TLS link still issued", body["tg"])
	}
}

func TestLinkWebProxyWithoutAMaskingDomain(t *testing.T) {
	h, r := webLinkInstall(t, true)

	// The proxy keeps running on the old config; only the link inputs change.
	if err := h.settings.Update(map[string]any{"mtproto.masking_domain": ""}); err != nil {
		t.Fatal(err)
	}

	// A WEB link alone is still worth handing over: the 409 is for neither.
	rec := mtprotoReq(t, r, http.MethodGet, "/api/mtproto/clients/alice/link", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body)
	}

	var body map[string]json.RawMessage

	decodeMtprotoData(t, rec, &body)

	if string(body["tg"]) != "null" || string(body["webproxy"]) == "null" {
		t.Errorf("body = %s, want tg null and webproxy set", rec.Body)
	}
}

func TestSyncDestReplansBeforeNaive(t *testing.T) {
	dest := &destStub{}
	dest.start(t)
	h, _, _, _ := destInstall(t, oneUser)

	calls := 0
	h.SetCaddyReplan(func() error {
		calls++

		return nil
	})

	if err := h.syncDest(); err != nil {
		t.Fatalf("syncDest: %v", err)
	}

	if calls != 1 {
		t.Errorf("replan called %d times, want 1", calls)
	}

	boom := errors.New("boom")
	h.SetCaddyReplan(func() error { return boom })

	err := h.syncDest()
	if !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "caddyfile: ") {
		t.Errorf("err = %v, want the replan error wrapped as caddyfile: ...", err)
	}
}

func TestUpdateWebListenOnABootstrappedInstallReplansCaddy(t *testing.T) {
	h, r := newMtprotoTestHandler(t)

	if err := h.settings.SetBootstrap("/panel-secret", "127.0.0.1:9443", "", 443); err != nil {
		t.Fatal(err)
	}

	calls := 0
	h.SetCaddyReplan(func() error {
		calls++

		return errors.New("dest says no")
	})

	// The front serves the stub site here, so switching the WEB proxy off
	// must leave it up and must not cost a re-plan.
	running := h.webFront.Listen()
	if rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto", `{"web_enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	if h.webFront.Listen() != running || calls != 0 {
		t.Fatalf("front listen = %q (want %q), replans = %d (want 0)", h.webFront.Listen(), running, calls)
	}

	addr := freeAddr(t)

	rec := mtprotoReq(t, r, http.MethodPut, "/api/mtproto", `{"web_listen":"`+addr+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with a warning, body = %s", rec.Code, rec.Body)
	}

	var body struct {
		WebListen string `json:"web_listen"`
		Warning   string `json:"warning"`
	}

	decodeMtprotoData(t, rec, &body)

	if calls != 1 || !strings.Contains(body.Warning, "dest says no") {
		t.Errorf("replans = %d, warning = %q; want one re-plan and its error surfaced", calls, body.Warning)
	}

	if body.WebListen != addr || h.webFront.Listen() != addr {
		t.Errorf("web_listen = %q, front = %q, want both %q", body.WebListen, h.webFront.Listen(), addr)
	}
}
