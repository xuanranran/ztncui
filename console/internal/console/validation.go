package console

import (
	"encoding/binary"
	"net/netip"
	"strings"
)

func validateNetwork(p Object, current Object) error {
	allowed := map[string]bool{"name": true, "description": true, "private": true, "v4AssignMode": true, "v6AssignMode": true, "routes": true, "ipAssignmentPools": true, "dns": true, "enableBroadcast": true, "multicastLimit": true, "mtu": true, "rules": true, "flowEditor": true, "tags": true, "capabilities": true, "policyMetadata": true}
	for k := range p {
		if !allowed[k] {
			return fail("不支持的网络设置: " + k)
		}
	}
	if err := validatePolicyDefinitions(p); err != nil {
		return err
	}
	if v, ok := p["name"]; ok {
		if trim(str(v)) == "" || len(str(v)) > 128 {
			return fail("网络名称不能为空，最多 128 字节")
		}
	}
	if v, ok := p["description"]; ok {
		if _, ok := v.(string); !ok || len(str(v)) > 4096 {
			return fail("网络描述过长")
		}
	}
	for _, k := range []string{"private", "enableBroadcast"} {
		if v, ok := p[k]; ok {
			if _, ok := v.(bool); !ok {
				return fail("开关参数无效")
			}
		}
	}
	for _, k := range []string{"multicastLimit", "mtu"} {
		if v, ok := p[k]; ok {
			if _, ok := v.(float64); !ok {
				if _, ok := v.(int); !ok {
					return fail("组播上限或 MTU 必须为整数")
				}
			}
			n := number(v)
			if n != float64(int(n)) || k == "multicastLimit" && (n < 0 || n > 4096) || k == "mtu" && (n < 1280 || n > 10000) {
				return fail("组播上限或 MTU 超出范围")
			}
		}
	}
	merged := clone(current)
	for k, v := range p {
		merged[k] = v
	}
	if hasAny(p, "routes", "ipAssignmentPools") {
		_, isArray := merged["routes"].([]any)
		_, isObjects := merged["routes"].([]Object)
		if !isArray && !isObjects {
			return fail("路由列表格式无效")
		}
		routes := objects(merged["routes"])
		if len(routes) > 128 {
			return fail("托管路由最多 128 条")
		}
		seen := map[string]bool{}
		locals := []netip.Prefix{}
		for _, r := range routes {
			target, err := netip.ParsePrefix(str(r["target"]))
			if err != nil {
				return fail("目标必须为有效 CIDR")
			}
			key := target.Masked().String()
			if seen[key] {
				return fail("托管路由目标重复")
			}
			seen[key] = true
			r["target"] = key
			if via := str(r["via"]); via != "" {
				ip, err := netip.ParseAddr(via)
				if err != nil || ip.Is4() != target.Addr().Is4() {
					return fail("网关地址与目标地址协议不一致")
				}
			} else {
				r["via"] = nil
				locals = append(locals, target.Masked())
			}
		}
		if _, ok := p["routes"]; ok {
			p["routes"] = routes
		}
		_, isArray = merged["ipAssignmentPools"].([]any)
		_, isObjects = merged["ipAssignmentPools"].([]Object)
		if !isArray && !isObjects {
			return fail("地址池列表格式无效")
		}
		pools := objects(merged["ipAssignmentPools"])
		for i, pool := range pools {
			start, e1 := netip.ParseAddr(str(pool["ipRangeStart"]))
			end, e2 := netip.ParseAddr(str(pool["ipRangeEnd"]))
			if e1 != nil || e2 != nil || start.Is4() != end.Is4() || start.Compare(end) > 0 {
				return fail("地址池起止地址无效")
			}
			contained := false
			for _, subnet := range locals {
				if subnet.Contains(start) && subnet.Contains(end) {
					if subnet.Addr().Is4() && subnet.Bits() <= 30 {
						network := subnet.Masked().Addr().As4()
						last := end.As4()
						broadcast := binary.BigEndian.Uint32(network[:]) | uint32((uint64(1)<<uint(32-subnet.Bits()))-1)
						if start == subnet.Masked().Addr() || binary.BigEndian.Uint32(last[:]) == broadcast {
							return fail("IPv4 自动分配范围不能包含网络地址或广播地址")
						}
					}
					contained = true
					break
				}
			}
			if !contained {
				return fail("地址池必须位于本地托管子网内")
			}
			for _, other := range pools[:i] {
				a, _ := netip.ParseAddr(str(other["ipRangeStart"]))
				b, _ := netip.ParseAddr(str(other["ipRangeEnd"]))
				if a.Is4() == start.Is4() && start.Compare(b) <= 0 && end.Compare(a) >= 0 {
					return fail("自动分配地址池范围重叠")
				}
			}
		}
	}
	if v, ok := p["dns"]; ok {
		dns, ok := v.(map[string]any)
		if !ok {
			return fail("DNS 格式无效")
		}
		domain := str(dns["domain"])
		if len(domain) > 253 || strings.ContainsAny(domain, " /:@\n\r\t") {
			return fail("搜索域格式无效")
		}
		servers, ok := dns["servers"].([]any)
		if !ok || len(servers) > 16 {
			return fail("DNS 服务器列表无效")
		}
		for _, v := range servers {
			if _, err := netip.ParseAddr(str(v)); err != nil {
				return fail("DNS 服务器必须为 IP 地址")
			}
		}
	}
	for _, key := range []string{"v4AssignMode", "v6AssignMode"} {
		if v, ok := p[key]; ok {
			m, ok := v.(map[string]any)
			if !ok {
				return fail("地址分配模式无效")
			}
			for k, b := range m {
				if k != "zt" && !(key == "v6AssignMode" && (k == "6plane" || k == "rfc4193")) {
					return fail("地址分配模式字段无效")
				}
				if _, ok := b.(bool); !ok {
					return fail("地址分配模式必须为开关")
				}
			}
		}
	}
	if v, ok := p["rules"]; ok {
		a, ok := v.([]any)
		if !ok || len(a) > 1024 {
			return fail("流规则必须为 JSON 数组，最多 1024 条")
		}
		for _, item := range a {
			m, ok := item.(map[string]any)
			if !ok || str(m["type"]) == "" {
				return fail("每条流规则需要 type 字段")
			}
		}
	}
	if v, ok := p["flowEditor"]; ok {
		editor, ok := v.(map[string]any)
		if !ok || p["rules"] == nil || digest(editor["rules"]) != digest(p["rules"]) {
			return fail("规则编辑状态必须与本次提交的规则一致")
		}
		if raw, exists := editor["policySnapshot"]; exists {
			snapshot, ok := raw.(map[string]any)
			if !ok {
				return fail("策略快照格式无效")
			}
			for _, key := range []string{"tags", "capabilities"} {
				value := current[key]
				if next, exists := p[key]; exists {
					value = next
				}
				if digest(snapshot[key]) != digest(value) {
					return fail("策略快照与提交的定义不一致")
				}
			}
		}
		mode := str(editor["mode"])
		source, sourceOK := editor["source"].(string)
		if mode != "template" && mode != "source" && mode != "json" || !sourceOK || len(source) > 32768 {
			return fail("规则编辑模式或源代码无效")
		}
		tpl, ok := editor["template"].(map[string]any)
		if !ok {
			return fail("规则模板格式无效")
		}
		for _, key := range []string{"layer2", "restrict"} {
			if _, ok := tpl[key].(bool); !ok {
				return fail("规则模板开关无效")
			}
		}
		services, ok := tpl["services"].([]any)
		if !ok || len(services) > 7 {
			return fail("服务列表无效")
		}
		for _, service := range services {
			switch service {
			case "ping", "smb", "dns", "http", "https", "ssh", "rdp":
			default:
				return fail("未知服务")
			}
		}
		ports, ok := tpl["ports"].([]any)
		if !ok || len(ports) > 64 {
			return fail("自定义端口列表无效")
		}
		for _, item := range ports {
			port, ok := item.(map[string]any)
			if !ok {
				return fail("自定义端口格式无效")
			}
			start, a := port["start"].(float64)
			end, b := port["end"].(float64)
			if !a || !b || start != float64(int(start)) || end != float64(int(end)) || start < 1 || end > 65535 || start > end || (port["protocol"] != "tcp" && port["protocol"] != "udp") {
				return fail("自定义端口范围无效")
			}
		}
		if raw, exists := tpl["excluded"]; exists {
			excluded, ok := raw.([]any)
			if !ok || len(excluded) > 128 {
				return fail("豁免设备列表无效，最多 128 台")
			}
			seen := map[string]bool{}
			for _, item := range excluded {
				id, ok := item.(string)
				if !ok || !validID(id, 10) || seen[id] {
					return fail("豁免设备 ID 无效或重复")
				}
				seen[id] = true
			}
		}
	}
	return nil
}
func validateMember(p Object) error {
	for k, v := range p {
		switch k {
		case "icon":
			icon, ok := v.(string)
			if !ok {
				return fail("设备图标无效")
			}
			switch icon {
			case "auto", "computer", "windows", "apple", "android", "linux", "docker", "freebsd", "openwrt", "nas", "phone":
			default:
				return fail("设备图标无效")
			}
		case "name":
			if _, ok := v.(string); !ok || len(str(v)) > 256 {
				return fail("设备名称无效")
			}
		case "tags":
			a, ok := v.([]any)
			if !ok || len(a) > 8 {
				return fail("标签最多 8 个")
			}
			for _, s := range a {
				if trim(str(s)) == "" || len(str(s)) > 64 {
					return fail("标签无效")
				}
			}
		case "pinned", "authorized", "activeBridge", "noAutoAssignIps":
			if _, ok := v.(bool); !ok {
				return fail("开关参数无效")
			}
		case "ipAssignments":
			a, ok := v.([]any)
			if !ok || len(a) > 32 {
				return fail("IP 地址列表无效")
			}
			for _, ip := range a {
				if _, err := netip.ParseAddr(str(ip)); err != nil {
					return fail("托管 IP 地址无效")
				}
			}
		default:
			return fail("不支持的设备设置: " + k)
		}
	}
	return nil
}
