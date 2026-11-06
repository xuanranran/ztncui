package console

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Demo                                           bool
	DisableProbes                                  bool
	Database, Address, TokenFile, Token, LegacyDir string
}
type Server struct {
	store           *Store
	controller      *Controller
	auth            *Auth
	demo            bool
	files           fs.FS
	opMu, refreshMu sync.Mutex
	mu              sync.RWMutex
	snapshot        Object
	clients         map[chan string]bool
	compressed      sync.Map
	probes          *probeManager
	disableProbes   bool
}

func New(cfg Config, files fs.FS) (*Server, error) {
	store, err := OpenStore(cfg.Database)
	if err != nil {
		return nil, err
	}
	mode := "live"
	if cfg.Demo {
		mode = "demo"
	}
	if previous := str(store.Get("runtime:mode")); previous != "" && previous != mode {
		store.Close()
		return nil, fail("预览与真实控制器必须使用不同的数据库")
	}
	if err = store.Set("runtime:mode", mode); err != nil {
		store.Close()
		return nil, err
	}
	if cfg.LegacyDir != "" {
		if err = store.ImportLegacy(cfg.LegacyDir); err != nil {
			store.Close()
			return nil, err
		}
	}
	c, err := NewController(cfg.Demo, cfg.Address, cfg.TokenFile, cfg.Token, store)
	if err != nil {
		store.Close()
		return nil, err
	}
	if cfg.Demo {
		if _, _, e := store.Admin(); e != nil {
			if err = store.CreateAdmin("admin", HashPassword("Preview123456!")); err != nil {
				store.Close()
				return nil, err
			}
		}
	}
	settings := defaultProbeSettings()
	if saved := store.Get(probeSettingsKey); saved != nil {
		settings, err = validateProbeSettings(objectOf(saved))
		if err != nil {
			store.Close()
			return nil, fail("无法加载 Ping 检测设置: " + err.Error())
		}
	}
	probes := newProbeManager(pingAddress)
	probes.configure(settings)
	return &Server{store: store, controller: c, auth: newAuth(), demo: cfg.Demo, files: files, clients: map[chan string]bool{}, probes: probes, disableProbes: cfg.DisableProbes}, nil
}
func (s *Server) Close() error {
	s.probes.Close()
	return s.store.Close()
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, Object{"error": message})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(v); err != nil {
		writeError(w, 400, "请求格式无效")
		return false
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		writeError(w, 400, "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}
func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:12])
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth", s.authStatus)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/logout", s.protected(s.logout))
	mux.HandleFunc("POST /api/password", s.protected(s.password))
	mux.HandleFunc("GET /api/overview", s.protected(s.overview))
	mux.HandleFunc("GET /api/networks/{nwid}", s.protected(s.network))
	mux.HandleFunc("POST /api/networks", s.protected(s.createNetwork))
	mux.HandleFunc("PATCH /api/networks/{nwid}", s.protected(s.patchNetwork))
	mux.HandleFunc("DELETE /api/networks/{nwid}", s.protected(s.deleteNetwork))
	mux.HandleFunc("POST /api/networks/{nwid}/members", s.protected(s.addMember))
	mux.HandleFunc("POST /api/networks/{nwid}/batch", s.protected(s.batch))
	mux.HandleFunc("POST /api/networks/{nwid}/probe", s.protected(s.probeMembers))
	mux.HandleFunc("POST /api/networks/{nwid}/members/{member}/probe", s.protected(s.probeMembers))
	mux.HandleFunc("PATCH /api/networks/{nwid}/members/{member}", s.protected(s.patchMember))
	mux.HandleFunc("PATCH /api/networks/{nwid}/members/{member}/policy", s.protected(s.patchMemberPolicy))
	mux.HandleFunc("DELETE /api/networks/{nwid}/members/{member}", s.protected(s.deleteMember))
	mux.HandleFunc("GET /api/events", s.protected(s.events))
	mux.HandleFunc("POST /api/refresh", s.protected(s.forceRefresh))
	mux.HandleFunc("GET /api/audit", s.protected(s.audit))
	mux.HandleFunc("GET /api/settings/probes", s.protected(s.getProbeSettings))
	mux.HandleFunc("PUT /api/settings/probes", s.protected(s.updateProbeSettings))
	mux.HandleFunc("GET /api/export", s.protected(s.export))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "接口不存在") })
	mux.HandleFunc("/", s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					writeError(w, 403, "请求来源不允许")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.auth.get(r)
		if !ok {
			writeError(w, 401, "请重新登录")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-CSRF-Token") != sess.CSRF {
			writeError(w, 403, "请求验证失败，请刷新后重试")
			return
		}
		if nwid := r.PathValue("nwid"); nwid != "" && !validID(nwid, 16) {
			writeError(w, 400, "网络 ID 无效")
			return
		}
		if id := r.PathValue("member"); id != "" && !validID(id, 10) {
			writeError(w, 400, "设备 ID 无效")
			return
		}
		next(w, r)
	}
}
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	name, _, err := s.store.Admin()
	sess, ok := s.auth.get(r)
	writeJSON(w, 200, Object{"authenticated": ok, "setupRequired": err != nil, "username": name, "csrf": sess.CSRF, "demo": s.demo})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.auth.allowed(ip) {
		writeError(w, 429, "尝试次数过多，请稍后再试")
		return
	}
	var v struct{ Username, Password string }
	if !readJSON(w, r, &v) {
		return
	}
	name, hash, err := s.store.Admin()
	if err != nil || v.Username != name || !VerifyPassword(hash, v.Password) {
		writeError(w, 401, "用户名或密码不正确")
		return
	}
	if strings.HasPrefix(hash, "$argon2i$") {
		if err = s.store.Password(HashPassword(v.Password)); err != nil {
			writeError(w, 500, "升级密码存储失败")
			return
		}
	}
	sess := s.auth.issue(w, r)
	s.auth.mu.Lock()
	delete(s.auth.attempts, ip)
	s.auth.mu.Unlock()
	s.store.Log("管理员登录", name)
	writeJSON(w, 200, Object{"username": name, "csrf": sess.CSRF})
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if _, _, err := s.store.Admin(); err == nil {
		writeError(w, 409, "管理员已经存在")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.auth.allowed(ip) {
		writeError(w, 429, "尝试次数过多")
		return
	}
	var v struct{ Password string }
	if !readJSON(w, r, &v) {
		return
	}
	if len(v.Password) < 10 || len(v.Password) > 256 {
		writeError(w, 400, "密码长度须为 10 至 256 字节")
		return
	}
	if err := s.store.CreateAdmin("admin", HashPassword(v.Password)); err != nil {
		writeError(w, 409, "管理员已经存在")
		return
	}
	sess := s.auth.issue(w, r)
	s.store.Log("初始化管理员", "admin")
	writeJSON(w, 201, Object{"username": "admin", "csrf": sess.CSRF})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.logout(w, r)
	writeJSON(w, 200, Object{"ok": true})
}
func (s *Server) password(w http.ResponseWriter, r *http.Request) {
	var v struct{ Current, Password string }
	if !readJSON(w, r, &v) {
		return
	}
	_, hash, err := s.store.Admin()
	if err != nil || !VerifyPassword(hash, v.Current) {
		writeError(w, 400, "当前密码不正确")
		return
	}
	if len(v.Password) < 10 || len(v.Password) > 256 {
		writeError(w, 400, "新密码长度须为 10 至 256 字节")
		return
	}
	if err = s.store.Password(HashPassword(v.Password)); err != nil {
		writeError(w, 500, "保存密码失败")
		return
	}
	s.auth.mu.Lock()
	s.auth.sessions = map[string]session{}
	s.auth.mu.Unlock()
	sess := s.auth.issue(w, r)
	s.store.Log("修改管理员密码", "admin")
	writeJSON(w, 200, Object{"csrf": sess.CSRF})
}
func (s *Server) Run(ctx context.Context) {
	s.refresh(ctx)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
			active := len(s.clients) > 0
			s.mu.RUnlock()
			if active {
				s.refresh(ctx)
			}
		}
	}
}
func (s *Server) refresh(ctx context.Context) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	status, err := s.controller.Status(ctx)
	if err != nil {
		s.refreshError(err)
		return
	}
	networks, err := s.controller.Networks(ctx)
	if err != nil {
		s.refreshError(err)
		return
	}
	peers, err := s.controller.Peers(ctx)
	if err != nil {
		s.refreshError(err)
		return
	}
	peerMap := map[string]Object{}
	for _, p := range peers {
		peerMap[str(p["address"])] = p
	}
	now := time.Now()
	for _, n := range networks {
		id := str(n["nwid"])
		if id == "" {
			id = str(n["id"])
		}
		n["nwid"] = id
		n["revision"] = digest(n)
		n["routes"] = objects(n["routes"])
		n["ipAssignmentPools"] = objects(n["ipAssignmentPools"])
		n["rules"] = objects(n["rules"])
		n["tags"] = objects(n["tags"])
		n["capabilities"] = objects(n["capabilities"])
		n["policyMetadata"] = s.store.Get("network:" + id + ":policyMetadata")
		n["flowEditor"] = s.store.Get("network:" + id + ":flowEditor")
		dns := objectOf(n["dns"])
		n["dns"] = Object{"domain": str(dns["domain"]), "servers": stringsOf(dns["servers"])}
		members, e := s.controller.Members(ctx, id)
		if e != nil {
			s.refreshError(e)
			return
		}
		pins := stringsOf(s.store.Get("network:" + id + ":pinnedMembers"))
		pinset := map[string]bool{}
		for _, p := range pins {
			pinset[p] = true
		}
		online, authorized := 0, 0
		for index, m := range members {
			mid := str(m["id"])
			if mid == "" {
				mid = str(m["address"])
			}
			m["id"] = mid
			m["policyRevision"] = digest(memberPolicy(m))
			m["ipAssignments"] = stringsOf(m["ipAssignments"])
			if name, ok := s.store.Get(mid).(string); ok {
				m["name"] = name
			}
			m["policyTags"] = policyArray(m["tags"])
			m["capabilities"] = policyArray(m["capabilities"])
			m["tags"] = stringsOf(s.store.Get("tags:" + id + ":" + mid))
			m["pinned"] = pinset[mid]
			m["icon"] = str(s.store.Get("icon:" + id + ":" + mid))
			m["originalIndex"] = index
			p := peerMap[mid]
			lastSeen := int64(number(m["lastSeen"]))
			if lastSeen < 0 || lastSeen > now.UnixMilli()+30000 {
				lastSeen = 0
			}
			// Embedded controllers omit lastSeen; peer paths expose local receive times.
			for _, path := range objects(p["paths"]) {
				received := int64(number(path["lastReceive"]))
				if received > lastSeen && received <= now.UnixMilli()+30000 {
					lastSeen = received
				}
			}
			if mid == str(status["address"]) && flag(status["online"]) {
				lastSeen = now.UnixMilli()
			}
			m["lastSeen"] = lastSeen
			recent := lastSeen > 0 && now.UnixMilli()-lastSeen < 120000
			m["online"] = recent
			m["connection"] = "unknown"
			m["latency"] = -1
			m["version"] = str(p["version"])
			if str(m["version"]) == "" && m["vMajor"] != nil && number(m["vMajor"]) >= 0 {
				m["version"] = fmt.Sprintf("%d.%d.%d", int(number(m["vMajor"])), int(number(m["vMinor"])), int(number(m["vRev"])))
			}
			paths := []string{}
			for _, p := range objects(p["paths"]) {
				if flag(p["active"]) && !flag(p["expired"]) {
					paths = append(paths, str(p["address"]))
				}
			}
			if recent && len(paths) > 0 {
				m["connection"] = "direct"
				m["latency"] = number(p["latency"])
			} else if recent && p != nil {
				m["connection"] = "relay"
			}
			m["paths"] = paths
			if s.demo {
				m["online"] = flag(m["_demoOnline"])
				m["version"] = str(m["_demoVersion"])
				if flag(m["online"]) {
					m["connection"] = "direct"
					m["latency"] = number(m["_demoLatency"])
					m["paths"] = []string{str(m["_demoPath"])}
				}
			}
			for k := range m {
				if strings.HasPrefix(k, "_demo") {
					delete(m, k)
				}
			}
			s.observeProbe(id, m, now)
			if flag(m["online"]) {
				online++
			}
			if flag(m["authorized"]) {
				authorized++
			}
		}
		n["description"] = str(s.store.Get("network:" + id + ":description"))
		n["members"] = members
		n["total"] = len(members)
		n["online"] = online
		n["authorized"] = authorized
		n["pending"] = len(members) - authorized
	}
	revision := digest(Object{"controller": status, "networks": networks})
	next := Object{"controller": status, "networks": networks, "revision": revision, "sampledAt": now.UTC().Format(time.RFC3339), "error": "", "demo": s.demo}
	s.mu.Lock()
	previous := str(s.snapshot["revision"])
	previousError := str(s.snapshot["error"])
	s.snapshot = next
	if revision != previous || previousError != "" {
		for ch := range s.clients {
			select {
			case ch <- revision:
			default:
			}
		}
	}
	s.mu.Unlock()
}
func (s *Server) refreshError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot == nil {
		s.snapshot = Object{"networks": []Object{}, "controller": Object{}, "revision": "", "demo": s.demo}
	}
	s.snapshot["error"] = err.Error()
	for ch := range s.clients {
		select {
		case ch <- "error":
		default:
		}
	}
}
func (s *Server) current() Object { s.mu.RLock(); defer s.mu.RUnlock(); return clone(s.snapshot) }
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	snapshot := s.current()
	if snapshot == nil {
		s.refresh(r.Context())
		snapshot = s.current()
	}
	for _, n := range objects(snapshot["networks"]) {
		delete(n, "members")
		delete(n, "rules")
	}
	writeJSON(w, 200, snapshot)
}
func (s *Server) network(w http.ResponseWriter, r *http.Request) {
	snapshot := s.current()
	if snapshot == nil {
		s.refresh(r.Context())
		snapshot = s.current()
	}
	for _, n := range objects(snapshot["networks"]) {
		if str(n["nwid"]) == r.PathValue("nwid") {
			writeJSON(w, 200, n)
			return
		}
	}
	writeError(w, 404, "网络不存在或控制器暂不可用")
}
func (s *Server) forceRefresh(w http.ResponseWriter, r *http.Request) {
	s.refresh(r.Context())
	writeJSON(w, 200, s.current())
}
func (s *Server) changed(ctx context.Context, action, target string) {
	s.store.Log(action, target)
	s.refresh(ctx)
}
func (s *Server) createNetwork(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var p Object
	if !readJSON(w, r, &p) {
		return
	}
	if err := validateNetwork(Object{"name": p["name"]}, Object{}); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	n, err := s.controller.CreateNetwork(r.Context(), trim(str(p["name"])))
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	s.changed(r.Context(), "创建网络", str(n["nwid"]))
	writeJSON(w, 201, n)
}
func (s *Server) patchNetwork(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	id := r.PathValue("nwid")
	var p Object
	if !readJSON(w, r, &p) {
		return
	}
	n, err := s.controller.Network(r.Context(), id)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	n["nwid"] = id
	if hasAny(p, "tags", "capabilities", "policyMetadata") && r.Header.Get("If-Match") == "" {
		writeError(w, 428, "保存原生策略需要当前网络版本")
		return
	}
	if expected := r.Header.Get("If-Match"); expected != "" && expected != digest(n) {
		writeError(w, 409, "网络设置已变化，请重新加载后保存")
		return
	}
	if err = validateNetwork(p, n); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.validatePolicyRemoval(r.Context(), id, p, n); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	description, hasDescription := p["description"]
	delete(p, "description")
	flowEditor, hasFlowEditor := p["flowEditor"]
	delete(p, "flowEditor")
	policyMetadata, hasPolicyMetadata := p["policyMetadata"]
	delete(p, "policyMetadata")
	if len(p) > 0 {
		if _, err = s.controller.PatchNetwork(r.Context(), id, p); err != nil {
			writeError(w, 502, err.Error())
			return
		}
	}
	if hasDescription {
		if err = s.store.Set("network:"+id+":description", description); err != nil {
			writeError(w, 500, "网络已保存，描述写入失败")
			return
		}
	}
	if hasFlowEditor {
		if err = s.store.Set("network:"+id+":flowEditor", flowEditor); err != nil {
			writeError(w, 500, "规则已保存，编辑器草稿存储失败，请重新加载")
			return
		}
	}
	if hasPolicyMetadata {
		if err = s.store.Set("network:"+id+":policyMetadata", policyMetadata); err != nil {
			writeError(w, 500, "策略已保存，名称写入失败")
			return
		}
	}
	action := "更新网络设置"
	if hasAny(p, "tags", "capabilities") || hasPolicyMetadata {
		action = "更新原生策略"
	}
	s.changed(r.Context(), action, id)
	writeJSON(w, 200, Object{"ok": true})
}
func (s *Server) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	id := r.PathValue("nwid")
	if err := s.controller.Delete(r.Context(), id, ""); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	s.changed(r.Context(), "删除网络", id)
	writeJSON(w, 200, Object{"ok": true})
}
func (s *Server) memberPatch(ctx context.Context, nwid, id string, p Object) error {
	found := false
	for _, network := range objects(s.current()["networks"]) {
		if str(network["nwid"]) == nwid {
			for _, member := range objects(network["members"]) {
				if str(member["id"]) == id {
					found = true
					break
				}
			}
		}
	}
	if !found {
		return fail("设备不存在，请刷新设备列表")
	}
	if err := validateMember(p); err != nil {
		return err
	}
	name, hasName := p["name"]
	icon, hasIcon := p["icon"]
	delete(p, "icon")
	tags, hasTags := p["tags"]
	pin, hasPin := p["pinned"]
	delete(p, "name")
	delete(p, "tags")
	delete(p, "pinned")
	if len(p) > 0 {
		if _, err := s.controller.PatchMember(ctx, nwid, id, p); err != nil {
			return err
		}
	}
	if hasName {
		if err := s.store.Set(id, name); err != nil {
			return err
		}
	}
	if hasIcon {
		if err := s.store.Set("icon:"+nwid+":"+id, icon); err != nil {
			return err
		}
	}
	if hasTags {
		if err := s.store.Set("tags:"+nwid+":"+id, tags); err != nil {
			return err
		}
	}
	if hasPin {
		key := "network:" + nwid + ":pinnedMembers"
		old := stringsOf(s.store.Get(key))
		next := []string{}
		for _, v := range old {
			if v != id {
				next = append(next, v)
			}
		}
		if flag(pin) {
			next = append(next, id)
		}
		if err := s.store.Set(key, next); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) patchMember(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var p Object
	if !readJSON(w, r, &p) {
		return
	}
	nwid, id := r.PathValue("nwid"), r.PathValue("member")
	if err := s.memberPatch(r.Context(), nwid, id, p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.changed(r.Context(), "更新设备", nwid+"/"+id)
	writeJSON(w, 200, Object{"ok": true})
}
func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var v struct {
		ID, Name   string
		Authorized bool
	}
	if !readJSON(w, r, &v) {
		return
	}
	if !validID(v.ID, 10) || len(v.Name) > 256 {
		writeError(w, 400, "设备 ID 应为 10 位小写十六进制")
		return
	}
	nwid := r.PathValue("nwid")
	members, err := s.controller.Members(r.Context(), nwid)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	for _, m := range members {
		if str(m["id"]) == v.ID {
			writeError(w, 409, "设备已存在")
			return
		}
	}
	if s.demo {
		err = s.store.PutRecord("member:"+nwid+":"+v.ID, Object{"id": v.ID, "address": v.ID, "authorized": v.Authorized, "ipAssignments": []string{}, "lastSeen": 0, "activeBridge": false, "noAutoAssignIps": false})
	} else {
		_, err = s.controller.PatchMember(r.Context(), nwid, v.ID, Object{"authorized": v.Authorized})
	}
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if err = s.store.Set(v.ID, v.Name); err != nil {
		writeError(w, 500, "保存设备名称失败")
		return
	}
	s.changed(r.Context(), "添加设备", nwid+"/"+v.ID)
	writeJSON(w, 201, Object{"ok": true})
}
func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	nwid, id := r.PathValue("nwid"), r.PathValue("member")
	if _, err := s.controller.PatchMember(r.Context(), nwid, id, Object{"authorized": false}); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if err := s.controller.Delete(r.Context(), nwid, id); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	s.changed(r.Context(), "删除设备", nwid+"/"+id)
	writeJSON(w, 200, Object{"ok": true})
}
func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var v struct {
		IDs   []string `json:"ids"`
		Patch Object   `json:"patch"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	if len(v.IDs) == 0 || len(v.IDs) > 100 {
		writeError(w, 400, "每次选择 1 至 100 个设备")
		return
	}
	for _, id := range v.IDs {
		if !validID(id, 10) {
			writeError(w, 400, "设备 ID 无效")
			return
		}
	}
	if err := validateMember(v.Patch); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	results := []Object{}
	for _, id := range v.IDs {
		err := s.memberPatch(r.Context(), r.PathValue("nwid"), id, clone(v.Patch))
		result := Object{"id": id, "ok": err == nil}
		if err != nil {
			result["error"] = err.Error()
		}
		results = append(results, result)
	}
	s.changed(r.Context(), "批量更新设备", r.PathValue("nwid"))
	writeJSON(w, 200, Object{"results": results})
}
func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	logs, err := s.store.Logs()
	if err != nil {
		writeError(w, 500, "读取操作记录失败")
		return
	}
	writeJSON(w, 200, logs)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", `attachment; filename="ztncui-management-export.json"`)
	data := s.current()
	for _, n := range objects(data["networks"]) {
		delete(n, "authTokens")
		delete(n, "ssoConfig")
		for _, m := range objects(n["members"]) {
			delete(m, "lastAuthorizedCredential")
			delete(m, "authenticationURL")
		}
	}
	data["exportedAt"] = time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, 200, data)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "无法建立实时连接")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan string, 1)
	s.mu.Lock()
	s.clients[ch] = true
	revision := str(s.snapshot["revision"])
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.clients, ch); s.mu.Unlock() }()
	if _, err := fmt.Fprintf(w, "event: update\ndata: %q\n\n", revision); err != nil {
		return
	}
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case rev := <-ch:
			if _, ok := s.auth.get(r); !ok {
				_, _ = fmt.Fprint(w, "event: expired\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			if _, err := fmt.Fprintf(w, "event: update\ndata: %q\n\n", rev); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, ok := s.auth.get(r); !ok {
				_, _ = fmt.Fprint(w, "event: expired\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.files, name)
	if err != nil {
		if strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
		data, err = fs.ReadFile(s.files, name)
	}
	if err != nil {
		http.Error(w, "Build the frontend first: cd web && npm ci && npm run build", 503)
		return
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("Vary", "Accept-Encoding")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(data) > 512 {
		// Only content-hashed assets are immutable; index.html changes on rebuild.
		cacheable := strings.HasPrefix(name, "assets/")
		cached, ok := s.compressed.Load(name)
		if !cacheable {
			ok = false
		}
		if !ok {
			var buffer bytes.Buffer
			writer, _ := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
			_, _ = writer.Write(data)
			_ = writer.Close()
			cached = buffer.Bytes()
			if cacheable {
				s.compressed.Store(name, cached)
			}
		}
		data = cached.([]byte)
		w.Header().Set("Content-Encoding", "gzip")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
func Serve(ctx context.Context, addr string, server *Server) error {
	httpServer := &http.Server{Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go server.Run(ctx)
	go func() {
		<-ctx.Done()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(stop)
	}()
	log.Printf("ztncui console listening on http://%s (demo=%t)", addr, server.demo)
	err := httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
