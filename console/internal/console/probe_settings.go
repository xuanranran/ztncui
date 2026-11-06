package console

import (
	"math"
	"net/http"
	"time"
)

const probeSettingsKey = "settings:probes"

type probeSettings struct {
	IntervalSeconds int `json:"intervalSeconds"`
	TimeoutMs       int `json:"timeoutMs"`
}

func defaultProbeSettings() probeSettings {
	return probeSettings{IntervalSeconds: 60, TimeoutMs: 1500}
}

func validateProbeSettings(body Object) (probeSettings, error) {
	for key := range body {
		if key != "intervalSeconds" && key != "timeoutMs" {
			return probeSettings{}, fail("不支持的 Ping 检测设置")
		}
	}
	values := []int{}
	for _, field := range []struct {
		key, label string
		min, max   int
	}{
		{"intervalSeconds", "检测间隔必须为 10–3600 秒的整数", 10, 3600},
		{"timeoutMs", "检测超时必须为 200–10000 毫秒的整数", 200, 10000},
	} {
		value, ok := body[field.key].(float64)
		if !ok || math.IsNaN(value) || value != math.Trunc(value) || value < float64(field.min) || value > float64(field.max) {
			return probeSettings{}, fail(field.label)
		}
		values = append(values, int(value))
	}
	return probeSettings{IntervalSeconds: values[0], TimeoutMs: values[1]}, nil
}

func (p *probeManager) settingsSnapshot() probeSettings {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settings
}

func (p *probeManager) configure(settings probeSettings) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.settings == settings {
		return
	}
	p.settings = settings
	for key, entry := range p.entries {
		if !entry.pending {
			entry.scheduled = time.Time{}
			p.entries[key] = entry
		}
	}
}

func (s *Server) probeSettingsResponse() Object {
	settings := s.probes.settingsSnapshot()
	return Object{"intervalSeconds": settings.IntervalSeconds, "timeoutMs": settings.TimeoutMs, "revision": digest(settings), "disabled": s.disableProbes}
}

func (s *Server) getProbeSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.probeSettingsResponse())
}

func (s *Server) updateProbeSettings(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var body Object
	if !readJSON(w, r, &body) {
		return
	}
	if r.Header.Get("If-Match") == "" {
		writeError(w, 428, "保存检测设置需要当前版本，请重新加载")
		return
	}
	if r.Header.Get("If-Match") != digest(s.probes.settingsSnapshot()) {
		writeError(w, 409, "检测设置已被其他会话修改，请重新加载后保存")
		return
	}
	settings, err := validateProbeSettings(body)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.store.Set(probeSettingsKey, settings); err != nil {
		writeError(w, 500, "检测设置保存失败")
		return
	}
	s.probes.configure(settings)
	s.changed(r.Context(), "更新 Ping 检测设置", "系统设置")
	writeJSON(w, 200, s.probeSettingsResponse())
}
