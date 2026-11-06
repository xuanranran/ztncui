package console

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitProbe(t *testing.T, p *probeManager, key string) probeResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result := p.result(key)
		if result.Status != "pending" && result.Status != "unprobed" {
			return result
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("probe did not complete")
	return probeResult{}
}

func TestProbeTargetsAreCanonicalAssignedPrivateIPs(t *testing.T) {
	member := Object{"id": "0123456789", "authorized": true, "ipAssignments": []string{"8.8.8.8", "127.0.0.1", "::1", "169.254.1.1", "224.0.0.1", "10.0.24.34; echo unsafe", "10.0.24.34/24", "::ffff:10.0.24.34", "fd00::34", "10.0.24.35"}}
	key, targets := probeTarget("0123456789abcdef", member)
	if len(targets) != 2 || targets[0].String() != "10.0.24.34" || targets[1].String() != "fd00::34" {
		t.Fatalf("unsafe or noncanonical targets: %v", targets)
	}
	member["authorized"] = false
	if k, values := probeTarget("0123456789abcdef", member); k != "" || len(values) != 0 {
		t.Fatal("unauthorized device was eligible")
	}
	member["authorized"] = true
	member["ipAssignments"] = []string{"10.0.24.36"}
	changed, _ := probeTarget("0123456789abcdef", member)
	if key == changed {
		t.Fatal("changed assignment reused old reachability evidence")
	}
}

func TestProbeQueueIsBoundedAndDeduplicated(t *testing.T) {
	var active, peak atomic.Int32
	gate := make(chan struct{})
	p := newProbeManager(func(ctx context.Context, _ netip.Addr) (time.Duration, error) {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		defer active.Add(-1)
		select {
		case <-gate:
			return 5 * time.Millisecond, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	})
	defer p.Close()
	targets := []netip.Addr{netip.MustParseAddr("10.0.24.34")}
	accepted := []string{}
	for i := 0; i < 100; i++ {
		key := string(rune('A' + i))
		if p.schedule(key, targets, time.Minute) {
			accepted = append(accepted, key)
		}
	}
	if len(accepted) > 68 || len(accepted) < 64 {
		t.Fatalf("unexpected queue bound: %d", len(accepted))
	}
	if p.schedule(accepted[0], targets, 0) {
		t.Fatal("duplicated pending job")
	}
	close(gate)
	for _, key := range accepted {
		if waitProbe(t, p, key).Status != "reachable" {
			t.Fatal("expected successful probe")
		}
	}
	if peak.Load() > 4 {
		t.Fatalf("too many concurrent probes: %d", peak.Load())
	}
	if p.schedule(accepted[0], targets, time.Minute) {
		t.Fatal("cooldown was bypassed")
	}
}

func TestProbeEvidenceDoesNotClaimDirectOrRelayConnection(t *testing.T) {
	s := testServer(t)
	s.probes.Close()
	s.probes = newProbeManager(func(context.Context, netip.Addr) (time.Duration, error) { return 36 * time.Millisecond, nil })
	s.demo = false
	member := Object{"id": "099d869dbd", "authorized": true, "ipAssignments": []string{"10.0.24.34"}, "online": false, "connection": "unknown", "lastSeen": 0}
	s.observeProbe("f47cef1d94743881", member, time.Now())
	key, _ := probeTarget("f47cef1d94743881", member)
	waitProbe(t, s.probes, key)
	s.observeProbe("f47cef1d94743881", member, time.Now())
	if !flag(member["online"]) || str(member["presenceSource"]) != "probe" || str(member["connection"]) != "unknown" {
		t.Fatalf("incorrect reachability evidence: %#v", member)
	}
	// Cached positive results expire, while their historical timestamp remains useful.
	s.probes.mu.Lock()
	entry := s.probes.entries[key]
	entry.result.LastSuccess = time.Now().Add(-2 * time.Minute).UnixMilli()
	s.probes.entries[key] = entry
	s.probes.mu.Unlock()
	member["online"] = false
	member["lastSeen"] = 0
	s.observeProbe("f47cef1d94743881", member, time.Now())
	if flag(member["online"]) || str(member["presenceState"]) != "unknown" || number(member["lastSeen"]) == 0 {
		t.Fatal("expired ping still marked device online or lost history")
	}
	member["ipAssignments"] = []string{"10.0.24.35"}
	member["lastSeen"] = 0
	member["authorized"] = false
	s.observeProbe("f47cef1d94743881", member, time.Now())
	if flag(member["online"]) || number(member["lastSeen"]) != 0 {
		t.Fatal("old IP evidence leaked into a changed or unauthorized device")
	}
}

func TestProbeFailureCannotOverrideNativeActivity(t *testing.T) {
	for _, failure := range []error{errNoProbeReply, errors.New("tool unavailable")} {
		s := testServer(t)
		s.probes.Close()
		s.probes = newProbeManager(func(context.Context, netip.Addr) (time.Duration, error) { return 0, failure })
		s.demo = false
		member := Object{"id": "099d869dbd", "authorized": true, "ipAssignments": []string{"10.0.24.34"}, "online": true, "connection": "direct", "lastSeen": time.Now().UnixMilli()}
		key, targets := probeTarget("f47cef1d94743881", member)
		s.probes.schedule(key, targets, time.Minute)
		result := waitProbe(t, s.probes, key)
		want := "no_response"
		if failure != errNoProbeReply {
			want = "unavailable"
		}
		if result.Status != want {
			t.Fatalf("incorrect failure classification: %#v", result)
		}
		s.observeProbe("f47cef1d94743881", member, time.Now())
		if !flag(member["online"]) || str(member["presenceSource"]) != "peer" {
			t.Fatal("ping failure overrode native activity")
		}
	}
}

func TestProbeRetainsLatencyOnlyForLastSuccessfulAddress(t *testing.T) {
	var attempts atomic.Int32
	p := newProbeManager(func(context.Context, netip.Addr) (time.Duration, error) {
		if attempts.Add(1) == 1 {
			return 37 * time.Millisecond, nil
		}
		return 0, errNoProbeReply
	})
	defer p.Close()
	addresses := []netip.Addr{netip.MustParseAddr("10.0.24.34")}
	p.schedule("member", addresses, 0)
	first := waitProbe(t, p, "member")
	if first.LastSuccessLatency != 37 || first.LastSuccessAddress != "10.0.24.34" {
		t.Fatalf("missing successful latency: %#v", first)
	}
	p.schedule("member", addresses, 0)
	failed := waitProbe(t, p, "member")
	if failed.Status != "no_response" || failed.Latency != -1 || failed.LastSuccessLatency != 37 || failed.LastSuccess != first.LastSuccess {
		t.Fatalf("failed probe overwrote last successful latency: %#v", failed)
	}
}

func TestProbeEndpointIsAuthenticatedScopedAndRateLimited(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	path := "/api/networks/f47cef1d94743881/members/0c1cb97eaa/probe"
	if w := c.request(t, "POST", path, nil); w.Code != 401 {
		t.Fatal("probe was not protected")
	}
	c.login(t)
	csrf := c.csrf
	c.csrf = ""
	if w := c.request(t, "POST", path, nil); w.Code != 403 {
		t.Fatal("probe bypassed CSRF")
	}
	c.csrf = csrf
	s.probes.Close()
	observed := make(chan string, 2)
	s.probes = newProbeManager(func(_ context.Context, address netip.Addr) (time.Duration, error) {
		observed <- address.String()
		return time.Millisecond, nil
	})
	s.demo = false
	w := c.request(t, "POST", path, Object{"address": "8.8.8.8"})
	if w.Code != 202 {
		t.Fatalf("manual probe failed: %d %s", w.Code, w.Body)
	}
	select {
	case address := <-observed:
		if address != "10.0.24.187" {
			t.Fatalf("probed supplied address instead of assigned IP: %s", address)
		}
	case <-time.After(time.Second):
		t.Fatal("manual probe did not run")
	}
	w = c.request(t, "POST", path, nil)
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"scheduled":0`) {
		t.Fatalf("manual probe bypassed cooldown: %s", w.Body)
	}
	if w := c.request(t, "POST", "/api/networks/f47cef1d94743881/members/0000000000/probe", nil); w.Code != 404 {
		t.Fatal("unknown device was accepted")
	}
	s.disableProbes = true
	if w := c.request(t, "POST", path, nil); w.Code != 403 {
		t.Fatal("disabled probes were accepted")
	}
}
