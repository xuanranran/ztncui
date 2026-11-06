package console

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func policyArray(v any) []any {
	data, _ := json.Marshal(v)
	var items []any
	_ = json.Unmarshal(data, &items)
	if items == nil {
		return []any{}
	}
	return items
}
func memberPolicy(m Object) Object {
	return Object{"tags": policyArray(m["tags"]), "capabilities": policyArray(m["capabilities"])}
}
func uint32Value(v any) bool {
	n, ok := v.(float64)
	return ok && n >= 0 && n <= 4294967295 && n == float64(uint32(n))
}
func validateRuleArray(v any, limit int) error {
	rules, ok := v.([]any)
	if !ok || len(rules) > limit {
		return fail("规则数组格式或数量无效")
	}
	for _, item := range rules {
		r, ok := item.(map[string]any)
		if !ok || str(r["type"]) == "" {
			return fail("每条规则必须包含 type")
		}
		switch str(r["type"]) {
		case "ACTION_DROP", "ACTION_ACCEPT", "ACTION_TEE", "ACTION_WATCH", "ACTION_REDIRECT", "ACTION_BREAK", "ACTION_PRIORITY",
			"MATCH_SOURCE_ZEROTIER_ADDRESS", "MATCH_DEST_ZEROTIER_ADDRESS", "MATCH_VLAN_ID", "MATCH_VLAN_PCP", "MATCH_VLAN_DEI", "MATCH_MAC_SOURCE", "MATCH_MAC_DEST", "MATCH_IPV4_SOURCE", "MATCH_IPV4_DEST", "MATCH_IPV6_SOURCE", "MATCH_IPV6_DEST", "MATCH_IP_TOS", "MATCH_IP_PROTOCOL", "MATCH_ETHERTYPE", "MATCH_ICMP", "MATCH_IP_SOURCE_PORT_RANGE", "MATCH_IP_DEST_PORT_RANGE", "MATCH_CHARACTERISTICS", "MATCH_FRAME_SIZE_RANGE", "MATCH_RANDOM", "MATCH_TAGS_DIFFERENCE", "MATCH_TAGS_BITWISE_AND", "MATCH_TAGS_BITWISE_OR", "MATCH_TAGS_BITWISE_XOR", "MATCH_TAGS_EQUAL", "MATCH_TAG_SENDER", "MATCH_TAG_RECEIVER", "MATCH_INTEGER_RANGE":
		default:
			return fail("未知规则类型: " + str(r["type"]))
		}
	}
	return nil
}
func validatePolicyDefinitions(p Object) error {
	for _, key := range []string{"tags", "capabilities"} {
		raw, exists := p[key]
		if !exists {
			continue
		}
		list, ok := raw.([]any)
		if !ok || len(list) > 128 {
			return fail("策略定义最多 128 项")
		}
		seen := map[float64]bool{}
		for _, raw := range list {
			d, ok := raw.(map[string]any)
			if !ok || !uint32Value(d["id"]) {
				return fail("策略 ID 必须为 0 至 4294967295 的整数")
			}
			id := number(d["id"])
			if seen[id] {
				return fail("策略 ID 重复")
			}
			seen[id] = true
			if key == "tags" {
				if d["default"] != nil && !uint32Value(d["default"]) {
					return fail("标签默认值必须为空或 32 位无符号整数")
				}
			} else {
				if _, ok := d["default"].(bool); !ok {
					return fail("能力默认分配必须为开关")
				}
				if err := validateRuleArray(d["rules"], 64); err != nil {
					return err
				}
			}
		}
	}
	if raw, exists := p["policyMetadata"]; exists {
		meta, ok := raw.(map[string]any)
		if !ok {
			return fail("策略名称格式无效")
		}
		for key, raw := range meta {
			if key != "tags" && key != "capabilities" {
				return fail("未知策略元数据")
			}
			entries, ok := raw.(map[string]any)
			if !ok || len(entries) > 128 {
				return fail("策略名称数量无效")
			}
			for id, raw := range entries {
				if _, err := strconv.ParseUint(id, 10, 32); err != nil {
					return fail("策略名称 ID 无效")
				}
				entry, ok := raw.(map[string]any)
				if !ok {
					return fail("策略名称格式无效")
				}
				name, ok := entry["name"].(string)
				if !ok || len(name) > 128 {
					return fail("策略名称过长")
				}
				if values, exists := entry["values"]; exists {
					choices, ok := values.(map[string]any)
					if !ok || len(choices) > 128 {
						return fail("标签枚举数量无效")
					}
					for value, label := range choices {
						if _, err := strconv.ParseUint(value, 10, 32); err != nil {
							return fail("标签枚举值无效")
						}
						if _, ok := label.(string); !ok || len(str(label)) > 128 {
							return fail("标签枚举名称无效")
						}
					}
				}
			}
		}
	}
	return nil
}
func (s *Server) validatePolicyRemoval(ctx context.Context, id string, patch Object, current Object) error {
	for _, key := range []string{"tags", "capabilities"} {
		if _, exists := patch[key]; !exists {
			continue
		}
		next := map[float64]bool{}
		for _, d := range objects(patch[key]) {
			next[number(d["id"])] = true
		}
		removed := map[float64]bool{}
		for _, d := range objects(current[key]) {
			if !next[number(d["id"])] {
				removed[number(d["id"])] = true
			}
		}
		if len(removed) == 0 {
			continue
		}
		if key == "tags" {
			rules := current["rules"]
			if nextRules, exists := patch["rules"]; exists {
				rules = nextRules
			}
			groups := []any{rules}
			caps := current["capabilities"]
			if nextCaps, exists := patch["capabilities"]; exists {
				caps = nextCaps
			}
			for _, cap := range objects(caps) {
				groups = append(groups, cap["rules"])
			}
			for _, group := range groups {
				for _, rule := range objects(group) {
					if strings.HasPrefix(str(rule["type"]), "MATCH_TAG") && removed[number(rule["id"])] {
						return fail("标签仍被流规则引用，请先移除对应规则")
					}
				}
			}
		}
		members, err := s.controller.Members(ctx, id)
		if err != nil {
			return err
		}
		for _, member := range members {
			for _, value := range policyArray(member[key]) {
				n := number(value)
				if key == "tags" {
					pair, ok := value.([]any)
					if !ok || len(pair) != 2 {
						continue
					}
					n = number(pair[0])
				}
				if removed[n] {
					return fail("策略仍有成员赋值，请先在成员分配中移除，再删除定义")
				}
			}
		}
	}
	return nil
}
func validateMemberPolicy(p, current, network Object) error {
	for key, raw := range p {
		if key != "policyTags" && key != "capabilities" {
			return fail("未知成员策略字段")
		}
		list, ok := raw.([]any)
		if !ok || len(list) > 128 {
			return fail("成员策略最多 128 项")
		}
		definitionKey := key
		if key == "policyTags" {
			definitionKey = "tags"
		}
		definitions := map[float64]bool{}
		for _, d := range objects(network[definitionKey]) {
			definitions[number(d["id"])] = true
		}
		old := map[float64]float64{}
		for _, item := range policyArray(current[definitionKey]) {
			if key == "policyTags" {
				pair, ok := item.([]any)
				if ok && len(pair) == 2 {
					old[number(pair[0])] = number(pair[1])
				}
			} else {
				old[number(item)] = 0
			}
		}
		seen := map[float64]bool{}
		for _, item := range list {
			id, value := item, any(float64(0))
			if key == "policyTags" {
				pair, ok := item.([]any)
				if !ok || len(pair) != 2 {
					return fail("策略标签必须为 [ID, 值]")
				}
				id, value = pair[0], pair[1]
			}
			if !uint32Value(id) || !uint32Value(value) {
				return fail("策略 ID 和值必须为 32 位无符号整数")
			}
			n := number(id)
			if seen[n] {
				return fail("成员策略 ID 重复")
			}
			seen[n] = true
			previous, existed := old[n]
			if !definitions[n] && (!existed || previous != number(value)) {
				return fail("不能分配未定义的策略")
			}
		}
	}
	return nil
}
func (s *Server) patchMemberPolicy(w http.ResponseWriter, r *http.Request) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var p Object
	if !readJSON(w, r, &p) {
		return
	}
	nwid, id := r.PathValue("nwid"), r.PathValue("member")
	network, err := s.controller.Network(r.Context(), nwid)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	network["nwid"] = nwid
	current, err := s.controller.Member(r.Context(), nwid, id)
	if err != nil {
		writeError(w, 404, "设备不存在或控制器不可用")
		return
	}
	if r.Header.Get("If-Match") == "" || r.Header.Get("If-Network-Match") == "" {
		writeError(w, 428, "保存成员策略需要当前网络和成员版本")
		return
	}
	if r.Header.Get("If-Match") != digest(memberPolicy(current)) || r.Header.Get("If-Network-Match") != digest(network) {
		writeError(w, 409, "策略已在其他位置更新，请重新加载后保存")
		return
	}
	if err := validateMemberPolicy(p, current, network); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	native := clone(p)
	if tags, exists := native["policyTags"]; exists {
		native["tags"] = tags
		delete(native, "policyTags")
	}
	if _, err := s.controller.PatchMember(r.Context(), nwid, id, native); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	s.changed(r.Context(), "更新成员策略", nwid+"/"+id)
	writeJSON(w, 200, Object{"ok": true})
}
