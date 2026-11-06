package console

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/netip"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var errNoProbeReply = errors.New("no ICMP reply")

type probeResult struct {
	Status             string `json:"status"`
	Address            string `json:"address"`
	CheckedAt          int64  `json:"checkedAt"`
	LastSuccess        int64  `json:"lastSuccess"`
	LastSuccessAddress string `json:"lastSuccessAddress"`
	LastSuccessLatency int64  `json:"lastSuccessLatency"`
	Latency            int64  `json:"latency"`
}

type probeTask struct {
	key       string
	addresses []netip.Addr
}

type probeEntry struct {
	result    probeResult
	scheduled time.Time
	pending   bool
}

// A bounded, shared queue prevents browser refreshes from spawning ping storms.
type probeManager struct {
	mu       sync.Mutex
	entries  map[string]probeEntry
	queue    chan probeTask
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	ping     func(context.Context, netip.Addr) (time.Duration, error)
	settings probeSettings
}

func newProbeManager(ping func(context.Context, netip.Addr) (time.Duration, error)) *probeManager {
	ctx, cancel := context.WithCancel(context.Background())
	p := &probeManager{entries: map[string]probeEntry{}, queue: make(chan probeTask, 64), ctx: ctx, cancel: cancel, ping: ping, settings: defaultProbeSettings()}
	for i := 0; i < 4; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

func (p *probeManager) Close() {
	p.cancel()
	p.wg.Wait()
}

func probeTarget(nwid string, member Object) (string, []netip.Addr) {
	if !flag(member["authorized"]) {
		return "", nil
	}
	addresses := []netip.Addr{}
	seen := map[netip.Addr]bool{}
	for _, raw := range stringsOf(member["ipAssignments"]) {
		address, err := netip.ParseAddr(raw)
		if err != nil {
			prefix, e := netip.ParsePrefix(raw)
			if e != nil {
				continue
			}
			address = prefix.Addr()
		}
		address = address.Unmap()
		if address.Zone() != "" || !address.IsPrivate() || !address.IsGlobalUnicast() || seen[address] {
			continue
		}
		seen[address] = true
		addresses = append(addresses, address)
		if len(addresses) == 2 {
			break
		}
	}
	if len(addresses) == 0 {
		return "", nil
	}
	parts := []string{nwid, str(member["id"])}
	for _, address := range addresses {
		parts = append(parts, address.String())
	}
	return strings.Join(parts, ":"), addresses
}

func (p *probeManager) result(key string) probeResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.entries[key]; ok {
		return entry.result
	}
	return probeResult{Status: "unprobed", Latency: -1}
}

func (p *probeManager) schedule(key string, addresses []netip.Addr, interval time.Duration) bool {
	if key == "" || len(addresses) == 0 || p.ctx.Err() != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	entry, exists := p.entries[key]
	if exists && (entry.pending || now.Sub(entry.scheduled) < interval) {
		return false
	}
	if len(p.entries) >= 4096 {
		for k, old := range p.entries {
			if !old.pending && now.Sub(old.scheduled) > 30*time.Minute {
				delete(p.entries, k)
			}
		}
		if !exists && len(p.entries) >= 4096 {
			return false
		}
	}
	select {
	case p.queue <- probeTask{key: key, addresses: addresses}:
		entry.pending = true
		entry.scheduled = now
		entry.result.Status = "pending"
		if !exists {
			entry.result.Latency = -1
			entry.result.LastSuccessLatency = -1
		}
		p.entries[key] = entry
		return true
	default:
		return false
	}
}

func (p *probeManager) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case task := <-p.queue:
			if p.ctx.Err() != nil {
				return
			}
			result := probeResult{Status: "no_response", Latency: -1}
			settings := p.settingsSnapshot()
			for _, address := range task.addresses {
				ctx, cancel := context.WithTimeout(p.ctx, time.Duration(settings.TimeoutMs)*time.Millisecond)
				latency, err := p.ping(ctx, address)
				cancel()
				result.Address = address.String()
				if err == nil {
					result.Status = "reachable"
					result.Latency = latency.Milliseconds()
					break
				}
				if !errors.Is(err, errNoProbeReply) && !errors.Is(err, context.DeadlineExceeded) {
					result.Status = "unavailable"
				}
			}
			if p.ctx.Err() != nil {
				return
			}
			result.CheckedAt = time.Now().UnixMilli()
			p.mu.Lock()
			entry := p.entries[task.key]
			result.LastSuccess = entry.result.LastSuccess
			result.LastSuccessAddress = entry.result.LastSuccessAddress
			result.LastSuccessLatency = entry.result.LastSuccessLatency
			if result.Status == "reachable" {
				result.LastSuccess = result.CheckedAt
				result.LastSuccessAddress = result.Address
				result.LastSuccessLatency = result.Latency
			}
			entry.result = result
			entry.pending = false
			p.entries[task.key] = entry
			p.mu.Unlock()
		}
	}
}

func pingAddress(ctx context.Context, address netip.Addr) (time.Duration, error) {
	if runtime.GOOS != "linux" {
		return 0, errors.New("ICMP probe is unsupported on this platform")
	}
	waitSeconds := 2
	if deadline, ok := ctx.Deadline(); ok {
		waitSeconds = max(1, int(math.Ceil(time.Until(deadline).Seconds())))
	}
	args := []string{"-n", "-c", "1", "-W", strconv.Itoa(waitSeconds), address.String()}
	started := time.Now()
	err := exec.CommandContext(ctx, "ping", args...).Run()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return 0, errNoProbeReply
		}
		return 0, err
	}
	return time.Since(started), nil
}

func (s *Server) observeProbe(nwid string, member Object, now time.Time) {
	member["presenceSource"] = "none"
	member["presenceState"] = "unknown"
	if flag(member["online"]) {
		member["presenceSource"] = "peer"
		member["presenceState"] = "online"
	}
	key, addresses := probeTarget(nwid, member)
	settings := s.probes.settingsSnapshot()
	if !s.demo && !s.disableProbes && !flag(member["online"]) {
		s.probes.schedule(key, addresses, time.Duration(settings.IntervalSeconds)*time.Second)
	}
	result := s.probes.result(key)
	if s.disableProbes {
		result.Status = "disabled"
	}
	member["probe"] = result
	if result.LastSuccess > int64(number(member["lastSeen"])) && result.LastSuccess <= now.UnixMilli()+30000 {
		member["lastSeen"] = result.LastSuccess
	}
	freshness := int64(settings.IntervalSeconds+30) * 1000
	if !flag(member["online"]) && result.LastSuccess > 0 && result.LastSuccess <= now.UnixMilli()+30000 && now.UnixMilli()-result.LastSuccess < freshness {
		member["online"] = true
		member["presenceSource"] = "probe"
		member["presenceState"] = "online"
	}
	if s.demo && !flag(member["online"]) {
		member["presenceState"] = "offline"
	}
}

func (s *Server) probeMembers(w http.ResponseWriter, r *http.Request) {
	if s.disableProbes {
		writeError(w, 403, "服务器已停用 ICMP 检测")
		return
	}
	nwid, memberID := r.PathValue("nwid"), r.PathValue("member")
	for _, network := range objects(s.current()["networks"]) {
		if str(network["nwid"]) != nwid {
			continue
		}
		scheduled, eligible, found := 0, 0, memberID == ""
		for _, member := range objects(network["members"]) {
			if memberID != "" && str(member["id"]) != memberID {
				continue
			}
			found = true
			key, addresses := probeTarget(nwid, member)
			if key != "" {
				eligible++
				if !s.demo && s.probes.schedule(key, addresses, 15*time.Second) {
					scheduled++
				}
			}
		}
		if !found {
			writeError(w, 404, "设备不存在")
			return
		}
		writeJSON(w, http.StatusAccepted, Object{"scheduled": scheduled, "eligible": eligible, "demo": s.demo})
		return
	}
	writeError(w, 404, "网络不存在或控制器暂不可用")
}
