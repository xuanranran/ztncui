package console

import (
	"context"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func TestProbeSettingsAuthValidationAndConflicts(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	path := "/api/settings/probes"
	if w := c.request(t, "GET", path, nil); w.Code != 401 {
		t.Fatal("unprotected settings")
	}
	c.login(t)
	initial := objectResponse(t, c.request(t, "GET", path, nil))
	if number(initial["intervalSeconds"]) != 60 || number(initial["timeoutMs"]) != 1500 {
		t.Fatalf("unexpected defaults: %#v", initial)
	}
	revision := str(initial["revision"])
	valid := Object{"intervalSeconds": 90, "timeoutMs": 2500}
	csrf := c.csrf
	c.csrf = ""
	if w := c.request(t, "PUT", path, valid, "If-Match", revision); w.Code != 403 {
		t.Fatal("settings bypassed CSRF")
	}
	c.csrf = csrf
	if w := c.request(t, "PUT", path, valid); w.Code != 428 {
		t.Fatal("unversioned update accepted")
	}
	for _, invalid := range []Object{
		{}, {"intervalSeconds": 60}, {"intervalSeconds": 9, "timeoutMs": 1500},
		{"intervalSeconds": 3601, "timeoutMs": 1500}, {"intervalSeconds": 10.5, "timeoutMs": 1500},
		{"intervalSeconds": "60", "timeoutMs": 1500}, {"intervalSeconds": nil, "timeoutMs": 1500},
		{"intervalSeconds": true, "timeoutMs": 1500}, {"intervalSeconds": 60, "timeoutMs": 199},
		{"intervalSeconds": 60, "timeoutMs": 10001}, {"intervalSeconds": 60, "timeoutMs": 250.5},
		{"intervalSeconds": 60, "timeoutMs": "1500"}, {"intervalSeconds": 60, "timeoutMs": 1500, "disabled": false},
	} {
		if w := c.request(t, "PUT", path, invalid, "If-Match", revision); w.Code != 400 {
			t.Fatalf("invalid settings accepted: %#v HTTP %d", invalid, w.Code)
		}
	}
	updated := objectResponse(t, c.request(t, "PUT", path, valid, "If-Match", revision))
	if number(updated["timeoutMs"]) != 2500 || s.probes.settingsSnapshot().IntervalSeconds != 90 {
		t.Fatal("settings did not take effect")
	}
	if w := c.request(t, "PUT", path, valid, "If-Match", revision); w.Code != 409 {
		t.Fatal("stale settings update accepted")
	}
	for _, boundary := range []Object{{"intervalSeconds": 10, "timeoutMs": 200}, {"intervalSeconds": 3600, "timeoutMs": 10000}} {
		updated = objectResponse(t, c.request(t, "PUT", path, boundary, "If-Match", str(updated["revision"])))
	}
	s.disableProbes = true
	if !flag(objectResponse(t, c.request(t, "GET", path, nil))["disabled"]) {
		t.Fatal("startup disable flag was hidden")
	}
}

func TestProbeSettingsSurviveRestart(t *testing.T) {
	cfg := Config{Demo: true, Database: filepath.Join(t.TempDir(), "settings.sqlite"), Address: "http://127.0.0.1:9993"}
	files := fstest.MapFS{"index.html": {Data: []byte("test")}}
	s, err := New(cfg, files)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	s.refresh(context.Background())
	c := &testClient{s: s}
	c.login(t)
	initial := objectResponse(t, c.request(t, "GET", "/api/settings/probes", nil))
	updated := objectResponse(t, c.request(t, "PUT", "/api/settings/probes", Object{"intervalSeconds": 120, "timeoutMs": 2000}, "If-Match", str(initial["revision"])))
	s.Close()
	s = nil
	s, err = New(cfg, files)
	if err != nil {
		t.Fatal(err)
	}
	c.s = s
	c.login(t)
	current := objectResponse(t, c.request(t, "GET", "/api/settings/probes", nil))
	if digest(updated) != digest(current) || s.probes.settingsSnapshot().TimeoutMs != 2000 {
		t.Fatalf("settings were lost after restart: %#v", current)
	}
	logs, err := s.store.Logs()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, log := range logs {
		found = found || str(log["action"]) == "更新 Ping 检测设置"
	}
	if !found {
		t.Fatal("settings update was not audited")
	}
}

func TestProbeWorkerUsesUpdatedTimeout(t *testing.T) {
	var attempts atomic.Int32
	deadlines := make(chan time.Duration, 2)
	p := newProbeManager(func(ctx context.Context, _ netip.Addr) (time.Duration, error) {
		deadline, _ := ctx.Deadline()
		deadlines <- time.Until(deadline)
		if attempts.Add(1) == 1 {
			<-ctx.Done()
			return 0, ctx.Err()
		}
		return time.Millisecond, nil
	})
	defer p.Close()
	addresses := []netip.Addr{netip.MustParseAddr("10.0.24.34")}
	p.configure(probeSettings{IntervalSeconds: 60, TimeoutMs: 200})
	p.schedule("device", addresses, 0)
	if result := waitProbe(t, p, "device"); result.Status != "no_response" {
		t.Fatalf("timeout was not respected: %#v", result)
	}
	first := <-deadlines
	if first < 100*time.Millisecond || first > 200*time.Millisecond {
		t.Fatalf("wrong short deadline: %v", first)
	}
	p.configure(probeSettings{IntervalSeconds: 60, TimeoutMs: 2500})
	p.schedule("device", addresses, 0)
	waitProbe(t, p, "device")
	second := <-deadlines
	if second < 2400*time.Millisecond || second > 2500*time.Millisecond {
		t.Fatalf("new timeout did not take effect: %v", second)
	}
}

func TestProbeIntervalControlsScheduleAndPositiveFreshness(t *testing.T) {
	s := testServer(t)
	s.probes.Close()
	s.probes = newProbeManager(func(ctx context.Context, _ netip.Addr) (time.Duration, error) { <-ctx.Done(); return 0, ctx.Err() })
	s.demo = false
	s.probes.configure(probeSettings{IntervalSeconds: 120, TimeoutMs: 200})
	member := Object{"id": "099d869dbd", "authorized": true, "ipAssignments": []string{"10.0.24.34"}, "online": false, "lastSeen": 0}
	key, _ := probeTarget("f47cef1d94743881", member)
	s.probes.mu.Lock()
	s.probes.entries[key] = probeEntry{scheduled: time.Now().Add(-70 * time.Second), result: probeResult{Status: "no_response", LastSuccess: time.Now().Add(-100 * time.Second).UnixMilli()}}
	s.probes.mu.Unlock()
	s.observeProbe("f47cef1d94743881", member, time.Now())
	if s.probes.result(key).Status != "no_response" || !flag(member["online"]) {
		t.Fatal("longer interval did not control scheduling and freshness")
	}
	s.probes.configure(probeSettings{IntervalSeconds: 10, TimeoutMs: 200})
	s.probes.mu.Lock()
	entry := s.probes.entries[key]
	if !entry.scheduled.IsZero() {
		t.Fatal("changed settings did not allow immediate rescheduling")
	}
	entry.scheduled = time.Now()
	s.probes.entries[key] = entry
	s.probes.mu.Unlock()
	member["online"] = false
	s.observeProbe("f47cef1d94743881", member, time.Now())
	if flag(member["online"]) {
		t.Fatal("short interval retained stale positive evidence")
	}
}
