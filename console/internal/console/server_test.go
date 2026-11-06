package console

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/argon2"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{Demo: true, Database: filepath.Join(t.TempDir(), "test.sqlite"), Address: "http://127.0.0.1:9993"}, fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>test</title>")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.refresh(context.Background())
	return s
}

type testClient struct {
	s      *Server
	cookie *http.Cookie
	csrf   string
}

func (c *testClient) request(t *testing.T, method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		reader = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, reader)
	r.RemoteAddr = "127.0.0.1:12345"
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	for i := 0; i < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(w, r)
	return w
}
func (c *testClient) login(t *testing.T) {
	t.Helper()
	w := c.request(t, "POST", "/api/login", Object{"username": "admin", "password": "Preview123456!"})
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var v Object
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	c.csrf = str(v["csrf"])
	c.cookie = w.Result().Cookies()[0]
}
func objectResponse(t *testing.T, w *httptest.ResponseRecorder) Object {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	var v Object
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAuthSingletonAndCSRF(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	if w := c.request(t, "GET", "/api/overview", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := c.request(t, "POST", "/api/setup", Object{"password": "another-password"}); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if _, err := s.store.db.Exec("INSERT INTO admin(id,username,hash) VALUES(2,'second','hash')"); err == nil {
		t.Fatal("second administrator accepted")
	}
	c.login(t)
	csrf := c.csrf
	c.csrf = ""
	if w := c.request(t, "POST", "/api/refresh", Object{}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	c.csrf = csrf
	if w := c.request(t, "POST", "/api/refresh", Object{}, "Origin", "https://evil.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := c.request(t, "POST", "/api/users", Object{}); w.Code != 404 {
		t.Fatal("unexpected user creation endpoint")
	}
}
func TestNetworkPatchPreservesOtherFieldsAndRejectsStaleEdits(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	id := "f47cef1d94743881"
	original := objectResponse(t, c.request(t, "GET", "/api/networks/"+id, nil))
	revision := str(original["revision"])
	w := c.request(t, "PATCH", "/api/networks/"+id, Object{"name": "保留原有网络", "description": "新备注"}, "If-Match", revision)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	next := objectResponse(t, c.request(t, "GET", "/api/networks/"+id, nil))
	for _, key := range []string{"nwid", "routes", "ipAssignmentPools", "members", "rules", "private"} {
		if digest(original[key]) != digest(next[key]) {
			t.Errorf("field changed: %s", key)
		}
	}
	if str(next["description"]) != "新备注" {
		t.Fatal("description not persisted")
	}
	if w = c.request(t, "PATCH", "/api/networks/"+id, Object{"name": "stale"}, "If-Match", revision); w.Code != 409 {
		t.Fatalf("stale edit accepted: %d", w.Code)
	}
}
func TestValidationPreventsInvalidRangesAndDNS(t *testing.T) {
	current := clone(demoNetwork("f47cef1d94743881", "test", "10.0.24"))
	cases := []Object{{"routes": []any{Object{"target": "10.0.24.0/24", "via": "::1"}}}, {"ipAssignmentPools": []any{Object{"ipRangeStart": "192.168.1.1", "ipRangeEnd": "192.168.1.254"}}}, {"ipAssignmentPools": []any{Object{"ipRangeStart": "10.0.24.20", "ipRangeEnd": "10.0.24.10"}}}, {"ipAssignmentPools": []any{Object{"ipRangeStart": "10.0.24.1", "ipRangeEnd": "10.0.24.20"}, Object{"ipRangeStart": "10.0.24.10", "ipRangeEnd": "10.0.24.30"}}}, {"dns": map[string]any{"domain": "bad/domain", "servers": []any{"8.8.8.8"}}}, {"private": "false"}, {"v6AssignMode": map[string]any{"zt": "true"}}}
	for _, p := range cases {
		if err := validateNetwork(p, current); err == nil {
			t.Errorf("invalid settings accepted: %#v", p)
		}
	}
}
func TestMemberMetadataBatchAndPersistence(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	base := "/api/networks/f47cef1d94743881"
	w := c.request(t, "PATCH", base+"/members/1996777016", Object{"name": "中文名称", "tags": []any{"测试"}, "pinned": true, "authorized": false})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	n := objectResponse(t, c.request(t, "GET", base, nil))
	found := false
	for _, m := range objects(n["members"]) {
		if str(m["id"]) == "1996777016" {
			found = true
			if str(m["name"]) != "中文名称" || !flag(m["pinned"]) || flag(m["authorized"]) {
				t.Fatal(m)
			}
		}
	}
	if !found {
		t.Fatal("member missing")
	}
	w = c.request(t, "POST", base+"/batch", Object{"ids": []string{"0c1cb97eaa", "0000000000"}, "patch": Object{"authorized": false}})
	results := objects(objectResponse(t, w)["results"])
	if len(results) != 2 || !flag(results[0]["ok"]) || flag(results[1]["ok"]) {
		t.Fatal("partial failures were not reported", results)
	}
	if str(s.store.Get("1996777016")) != "中文名称" {
		t.Fatal("metadata not persisted")
	}
}
func TestAddDeleteAndCreateFlow(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	base := "/api/networks/f47cef1d94743881"
	if w := c.request(t, "POST", base+"/members", Object{"id": "123456789a", "name": "新设备", "authorized": true}); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := c.request(t, "POST", base+"/members", Object{"id": "123456789a"}); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := c.request(t, "DELETE", base+"/members/123456789a", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := c.request(t, "POST", "/api/networks", Object{"name": "新网络"})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	var n Object
	_ = json.Unmarshal(w.Body.Bytes(), &n)
	id := str(n["nwid"])
	if !validID(id, 16) {
		t.Fatal(id)
	}
	if w = c.request(t, "DELETE", "/api/networks/"+id, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestLegacyImportIsReadOnlyAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "storage"), 0700); err != nil {
		t.Fatal(err)
	}
	passwd := []byte(`{"admin":{"name":"admin","hash":"legacy-hash"},"other":{"name":"other","hash":"ignored"}}`)
	files := map[string][]byte{"passwd": passwd, "storage/one": []byte(`{"key":"0c1cb97eaa","value":"旧设备名称"}`), "storage/two": []byte(`{"key":"network:f47cef1d94743881:pinnedMembers","value":["0c1cb97eaa"]}`)}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "next.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.ImportLegacy(dir); err != nil {
		t.Fatal(err)
	}
	name, hash, err := store.Admin()
	if err != nil || name != "admin" || hash != "legacy-hash" {
		t.Fatal(name, hash, err)
	}
	if str(store.Get("0c1cb97eaa")) != "旧设备名称" {
		t.Fatal("legacy name missing")
	}
	_ = store.Set("0c1cb97eaa", "新的名称")
	if err = store.ImportLegacy(dir); err != nil {
		t.Fatal(err)
	}
	if str(store.Get("0c1cb97eaa")) != "新的名称" {
		t.Fatal("repeat import overwrote metadata")
	}
	for name, expected := range files {
		actual, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil || !bytes.Equal(actual, expected) {
			t.Fatal("legacy source modified", name)
		}
	}
}
func TestLegacyArgon2AndSessionRevocation(t *testing.T) {
	salt := []byte("legacy-salt-1234")
	key := argon2.Key([]byte("legacy-password"), salt, 3, 4096, 1, 32)
	enc := base64.RawStdEncoding.EncodeToString
	hash := "$argon2i$v=19$m=4096,t=3,p=1$" + enc(salt) + "$" + enc(key)
	if !VerifyPassword(hash, "legacy-password") || VerifyPassword(hash, "wrong") {
		t.Fatal("legacy argon2i verification failed")
	}
	s := testServer(t)
	first, second := &testClient{s: s}, &testClient{s: s}
	first.login(t)
	second.login(t)
	w := first.request(t, "POST", "/api/password", Object{"current": "Preview123456!", "password": "ChangedPassword123!"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w = second.request(t, "GET", "/api/overview", nil); w.Code != 401 {
		t.Fatal("other session still valid")
	}
	if w = first.request(t, "GET", "/api/export", nil); w.Code != 401 {
		t.Fatal("old session still valid")
	}
}
func TestSSEDeliversMutations(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/api/events", nil)
	req.AddCookie(c.cookie)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buffer := make([]byte, 4096)
	n, err := response.Body.Read(buffer)
	if err != nil || !strings.Contains(string(buffer[:n]), "event: update") {
		t.Fatal("initial stream update missing", err)
	}
	if w := c.request(t, "PATCH", "/api/networks/f47cef1d94743881/members/0c1cb97eaa", Object{"pinned": false}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	n, err = response.Body.Read(buffer)
	if err != nil || !strings.Contains(string(buffer[:n]), "event: update") {
		t.Fatal("mutation was not pushed", err)
	}
}
func TestExportDoesNotLeakCredentials(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	w := c.request(t, "GET", "/api/export", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, secret := range []string{"$argon2", "Preview123456!", "csrf", "ztncui_session"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("export leaks credentials", secret)
		}
	}
}

func TestStaticRootAndClientRoutes(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	for _, path := range []string{"/", "/networks", "/network/f47cef1d94743881", "/settings"} {
		w := c.request(t, "GET", path, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<!doctype html>") {
			t.Errorf("SPA route %s: %d", path, w.Code)
		}
	}
	if w := c.request(t, "GET", "/assets/missing.js", nil); w.Code != 404 {
		t.Fatal("missing asset was served as HTML")
	}
}

func TestLiveAdapterCachesAndPreservesControllerFields(t *testing.T) {
	var mu sync.Mutex
	network := clone(demoNetwork("f47cef1d94743881", "existing", "10.0.24"))
	network["unknownFutureField"] = "preserved"
	network["dns"] = Object{}
	member := Object{"id": "0c1cb97eaa", "name": "native name", "tags": []any{[]any{7, 42}}, "vMajor": 1, "vMinor": 16, "vRev": 2, "authorized": true, "ipAssignments": nil, "lastSeen": time.Now().UnixMilli()}
	memberReads := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-ZT1-Auth") != "test-token" {
			writeError(w, 403, "token missing")
			return
		}
		switch r.URL.Path {
		case "/status":
			writeJSON(w, 200, Object{"address": "f47cef1d94", "online": true, "version": "1.16.2"})
		case "/peer":
			writeJSON(w, 200, []Object{})
		case "/controller/network":
			writeJSON(w, 200, []string{"f47cef1d94743881"})
		case "/controller/network/f47cef1d94743881":
			if r.Method == "POST" {
				var patch Object
				_ = json.NewDecoder(r.Body).Decode(&patch)
				for k, v := range patch {
					network[k] = v
				}
			}
			writeJSON(w, 200, network)
		case "/controller/network/f47cef1d94743881/member":
			writeJSON(w, 200, Object{"0c1cb97eaa": 1})
		case "/controller/network/f47cef1d94743881/member/0c1cb97eaa":
			if r.Method == "POST" {
				var patch Object
				_ = json.NewDecoder(r.Body).Decode(&patch)
				for k, v := range patch {
					member[k] = v
				}
			} else {
				memberReads++
			}
			writeJSON(w, 200, member)
		default:
			writeError(w, 404, "not found")
		}
	}))
	defer upstream.Close()
	s, err := New(Config{Database: filepath.Join(t.TempDir(), "live.sqlite"), Address: upstream.URL, Token: "test-token"}, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.store.CreateAdmin("admin", HashPassword("Preview123456!"))
	s.refresh(context.Background())
	s.refresh(context.Background())
	mu.Lock()
	reads := memberReads
	mu.Unlock()
	if reads != 1 {
		t.Fatalf("unchanged member was fetched %d times", reads)
	}
	c := &testClient{s: s}
	c.login(t)
	view := objectResponse(t, c.request(t, "GET", "/api/networks/f47cef1d94743881", nil))
	memberView := objects(view["members"])[0]
	if str(memberView["name"]) != "native name" || str(memberView["version"]) != "1.16.2" || len(memberView["policyTags"].([]any)) != 1 || len(stringsOf(memberView["tags"])) != 0 {
		t.Fatal("native name, version or policy tags were overwritten", memberView)
	}
	if stringsOf(objectOf(view["dns"])["servers"]) == nil || objects(view["members"])[0]["ipAssignments"] == nil {
		t.Fatal("empty controller arrays were not normalized")
	}
	w := c.request(t, "PATCH", "/api/networks/f47cef1d94743881", Object{"name": "renamed"}, "If-Match", str(view["revision"]))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	mu.Lock()
	preserved := str(network["unknownFutureField"])
	mu.Unlock()
	if preserved != "preserved" {
		t.Fatal("controller data overwritten")
	}
	w = c.request(t, "PATCH", "/api/networks/f47cef1d94743881/members/0c1cb97eaa", Object{"authorized": false})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	view = objectResponse(t, c.request(t, "GET", "/api/networks/f47cef1d94743881", nil))
	if flag(objects(view["members"])[0]["authorized"]) {
		t.Fatal("mutation did not invalidate member cache")
	}
}

func TestCompressedHTMLTracksFrontendRebuild(t *testing.T) {
	s := testServer(t)
	files := fstest.MapFS{"index.html": {Data: []byte(strings.Repeat("a", 600))}}
	s.files = files
	for _, content := range []string{strings.Repeat("a", 600), strings.Repeat("b", 600)} {
		files["index.html"].Data = []byte(content)
		r := httptest.NewRequest("GET", "/networks", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		s.static(w, r)
		reader, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || string(data) != content {
			t.Fatal("stale compressed HTML after frontend rebuild")
		}
	}
}

func TestFlowEditorMetadataDoesNotOverwriteNativePolicy(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	id := "f47cef1d94743881"
	policy := Object{"capabilities": []Object{{"id": 9, "rules": []Object{{"type": "ACTION_ACCEPT"}}}}, "tags": []Object{{"id": 7, "default": 42}}}
	if _, err := s.controller.PatchNetwork(context.Background(), id, policy); err != nil {
		t.Fatal(err)
	}
	rules := []Object{{"type": "ACTION_DROP"}}
	editor := Object{"mode": "source", "source": "drop;", "rules": rules, "template": Object{"layer2": false, "restrict": false, "services": []string{}, "ports": []Object{}}}
	w := c.request(t, "PATCH", "/api/networks/"+id, Object{"rules": rules, "flowEditor": editor})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	native, err := s.controller.Network(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if native["flowEditor"] != nil || digest(native["tags"]) != digest(policy["tags"]) || digest(native["capabilities"]) != digest(policy["capabilities"]) {
		t.Fatal("native policy was overwritten", native)
	}
	view := objectResponse(t, c.request(t, "GET", "/api/networks/"+id, nil))
	if str(objectOf(view["flowEditor"])["source"]) != "drop;" {
		t.Fatal("source was not persisted")
	}
	if str(objectOf(s.store.Get("network:" + id + ":flowEditor"))["source"]) != "drop;" {
		t.Fatal("source was not stored separately")
	}
	for _, patch := range []Object{
		{"flowEditor": editor},
		{"rules": []Object{{"type": "ACTION_ACCEPT"}}, "flowEditor": editor},
		{"rules": rules, "flowEditor": Object{"mode": "source", "rules": rules, "source": "drop;", "template": Object{}}},
	} {
		if w := c.request(t, "PATCH", "/api/networks/"+id, patch); w.Code != 400 {
			t.Fatal("invalid editor metadata accepted", w.Code, w.Body)
		}
	}
}

func TestFlowExemptionsAndRestorePreserveNetworkAndMemberSettings(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	id := "f47cef1d94743881"
	path := "/api/networks/" + id
	before := objectResponse(t, c.request(t, "GET", path, nil))
	template := Object{"layer2": true, "restrict": true, "services": []string{"ssh"}, "ports": []Object{}, "excluded": []string{"0c1cb97eaa"}}
	rules := []Object{{"type": "MATCH_SOURCE_ZEROTIER_ADDRESS", "zt": "0c1cb97eaa", "not": false, "or": false}, {"type": "MATCH_DEST_ZEROTIER_ADDRESS", "zt": "0c1cb97eaa", "not": false, "or": true}, {"type": "ACTION_ACCEPT"}, {"type": "ACTION_DROP"}}
	editor := Object{"mode": "template", "source": "accept;", "rules": rules, "template": template}
	patch := Object{"rules": rules, "flowEditor": editor}
	if w := c.request(t, "PATCH", path, patch, "If-Match", str(before["revision"])); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	saved := objectResponse(t, c.request(t, "GET", path, nil))
	if digest(objectOf(objectOf(saved["flowEditor"])["template"])["excluded"]) != digest([]string{"0c1cb97eaa"}) {
		t.Fatal("exemptions were not persisted")
	}
	for _, invalid := range []any{"0c1cb97eaa", []any{"short"}, []any{"0c1cb97eaa", "0c1cb97eaa"}, []any{42}, []any{Object{"id": "0c1cb97eaa"}}} {
		template["excluded"] = invalid
		if w := c.request(t, "PATCH", path, patch); w.Code != 400 {
			t.Fatal("invalid exemption list accepted", w.Code, w.Body)
		}
	}
	defaults := []Object{{"type": "ACTION_ACCEPT"}}
	restore := Object{"rules": defaults, "flowEditor": Object{"mode": "template", "source": "accept;", "rules": defaults, "template": Object{"layer2": false, "restrict": false, "services": []string{}, "ports": []Object{}, "excluded": []string{}}}}
	if w := c.request(t, "PATCH", path, restore, "If-Match", str(before["revision"])); w.Code != 409 {
		t.Fatal("restore ignored stale revision", w.Code)
	}
	if w := c.request(t, "PATCH", path, restore, "If-Match", str(saved["revision"])); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	after := objectResponse(t, c.request(t, "GET", path, nil))
	if digest(after["rules"]) != digest(defaults) || len(stringsOf(objectOf(objectOf(after["flowEditor"])["template"])["excluded"])) != 0 {
		t.Fatal("restore did not clear restrictions")
	}
	for _, key := range []string{"name", "routes", "ipAssignmentPools", "dns", "private", "members"} {
		if digest(before[key]) != digest(after[key]) {
			t.Fatal("restore modified unrelated setting", key)
		}
	}
}

func TestDemoAndLiveDatabasesCannotBeMixed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "console.sqlite")
	s, err := New(Config{Demo: true, Database: file, Address: "http://127.0.0.1:9993"}, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	other, err := New(Config{Database: file, Address: "http://127.0.0.1:9993"}, fstest.MapFS{})
	if err == nil {
		other.Close()
		t.Fatal("sample database accepted in live mode")
	}
}

func TestControllerRecoveryIsBroadcastWithoutDataChanges(t *testing.T) {
	s := testServer(t)
	ch := make(chan string, 1)
	s.clients[ch] = true
	s.refreshError(fail("temporary failure"))
	<-ch
	s.refresh(context.Background())
	select {
	case <-ch:
	default:
		t.Fatal("controller recovery was not broadcast")
	}
	if str(s.current()["error"]) != "" {
		t.Fatal("stale controller error retained")
	}
}
