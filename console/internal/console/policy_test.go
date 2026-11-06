package console

import (
	"context"
	"strings"
	"testing"
)

const policyNetwork = "f47cef1d94743881"
const policyMember = "0c1cb97eaa"

func policyView(t *testing.T, c *testClient) Object {
	t.Helper()
	return objectResponse(t, c.request(t, "GET", "/api/networks/"+policyNetwork, nil))
}
func policyUpdate(t *testing.T, c *testClient, patch Object) {
	t.Helper()
	view := policyView(t, c)
	w := c.request(t, "PATCH", "/api/networks/"+policyNetwork, patch, "If-Match", str(view["revision"]))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
}
func policyMemberView(n Object) Object {
	for _, m := range objects(n["members"]) {
		if str(m["id"]) == policyMember {
			return m
		}
	}
	return Object{}
}
func TestNativePolicyDefinitionsAndLimits(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	patch := Object{"tags": []Object{{"id": 7, "default": 0}, {"id": 4294967295, "default": nil}}, "capabilities": []Object{{"id": 9, "default": false, "rules": []Object{{"type": "ACTION_ACCEPT"}}}}, "policyMetadata": Object{"tags": Object{"7": Object{"name": "部门", "values": Object{"0": "未分类", "100": "工程"}}}, "capabilities": Object{"9": Object{"name": "SSH"}}}}
	if w := c.request(t, "PATCH", "/api/networks/"+policyNetwork, patch); w.Code != 428 {
		t.Fatal("missing revision accepted", w.Code)
	}
	policyUpdate(t, c, patch)
	native, err := s.controller.Network(context.Background(), policyNetwork)
	if err != nil {
		t.Fatal(err)
	}
	if native["policyMetadata"] != nil {
		t.Fatal("console labels sent to controller")
	}
	meta := objectOf(objectOf(objectOf(policyView(t, c)["policyMetadata"])["tags"])["7"])
	if str(meta["name"]) != "部门" {
		t.Fatal("policy names not persisted", meta)
	}
	invalid := []Object{
		{"tags": []Object{{"id": -1, "default": nil}}}, {"tags": []Object{{"id": 1.5, "default": nil}}}, {"tags": []Object{{"id": 4294967296, "default": nil}}},
		{"tags": []Object{{"id": 1, "default": -1}}}, {"tags": []Object{{"id": 1, "default": nil}, {"id": 1, "default": nil}}},
		{"capabilities": []Object{{"id": 9, "default": true, "rules": []any{"invalid"}}}},
		{"capabilities": []Object{{"id": 9, "default": true, "rules": []Object{{"type": "NO_SUCH_RULE"}}}}},
		{"policyMetadata": Object{"tags": Object{"7": Object{"name": 42}}}},
	}
	many := []Object{}
	for range 65 {
		many = append(many, Object{"type": "ACTION_ACCEPT"})
	}
	invalid = append(invalid, Object{"capabilities": []Object{{"id": 9, "default": true, "rules": many}}})
	for _, p := range invalid {
		n := policyView(t, c)
		w := c.request(t, "PATCH", "/api/networks/"+policyNetwork, p, "If-Match", str(n["revision"]))
		if w.Code != 400 {
			t.Fatal("invalid definition accepted", w.Code, w.Body)
		}
	}
}
func TestMemberPolicyConcurrencyAndTextTagIsolation(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	policyUpdate(t, c, Object{"tags": []Object{{"id": 7, "default": nil}}, "capabilities": []Object{{"id": 9, "default": false, "rules": []Object{{"type": "ACTION_ACCEPT"}}}}})
	path := "/api/networks/" + policyNetwork + "/members/" + policyMember
	if w := c.request(t, "PATCH", path, Object{"tags": []string{"文字标签"}}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	before := policyView(t, c)
	m := policyMemberView(before)
	patch := Object{"policyTags": []any{[]any{7, 0}}, "capabilities": []int{9}}
	if w := c.request(t, "PATCH", path+"/policy", patch); w.Code != 428 {
		t.Fatal("unguarded policy save accepted", w.Code)
	}
	headers := []string{"If-Match", str(m["policyRevision"]), "If-Network-Match", str(before["revision"])}
	if w := c.request(t, "PATCH", path+"/policy", patch, headers...); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	after := policyView(t, c)
	n := policyMemberView(after)
	if len(policyArray(n["policyTags"])) != 1 || len(policyArray(n["capabilities"])) != 1 || stringsOf(n["tags"])[0] != "文字标签" {
		t.Fatal("native and text tags mixed", n)
	}
	for _, key := range []string{"name", "authorized", "activeBridge", "ipAssignments", "pinned"} {
		if digest(m[key]) != digest(n[key]) {
			t.Fatal("unrelated member field changed", key)
		}
	}
	if w := c.request(t, "PATCH", path+"/policy", Object{"capabilities": []int{}}, headers...); w.Code != 409 {
		t.Fatal("stale policy save accepted", w.Code)
	}
	for _, bad := range []Object{{"policyTags": []any{[]any{7, -1}}}, {"policyTags": []any{[]any{7, 1}, []any{7, 2}}}, {"policyTags": []any{[]any{88, 1}}}, {"capabilities": []int{88}}, {"capabilities": []any{"9"}}, {"tags": []string{"wrong namespace"}}} {
		view := policyView(t, c)
		v := policyMemberView(view)
		w := c.request(t, "PATCH", path+"/policy", bad, "If-Match", str(v["policyRevision"]), "If-Network-Match", str(view["revision"]))
		if w.Code != 400 {
			t.Fatal("invalid member policy accepted", w.Code, w.Body)
		}
	}
	view := policyView(t, c)
	w := c.request(t, "PATCH", "/api/networks/"+policyNetwork, Object{"capabilities": []any{}}, "If-Match", str(view["revision"]))
	if w.Code != 400 {
		t.Fatal("assigned definition deleted", w.Code)
	}
}
func TestNativeTagRuleReferenceProtectsDeletion(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	rules := []Object{{"type": "MATCH_TAG_SENDER", "id": 7, "value": 100}, {"type": "ACTION_ACCEPT"}}
	policyUpdate(t, c, Object{"tags": []Object{{"id": 7, "default": nil}}, "rules": rules})
	n := policyView(t, c)
	if w := c.request(t, "PATCH", "/api/networks/"+policyNetwork, Object{"tags": []any{}}, "If-Match", str(n["revision"])); w.Code != 400 {
		t.Fatal("referenced tag deleted", w.Code)
	}
	policyUpdate(t, c, Object{"rules": []Object{{"type": "ACTION_BREAK"}}, "capabilities": []Object{{"id": 9, "default": false, "rules": rules}}})
	n = policyView(t, c)
	if w := c.request(t, "PATCH", "/api/networks/"+policyNetwork, Object{"tags": []any{}}, "If-Match", str(n["revision"])); w.Code != 400 {
		t.Fatal("capability-referenced tag deleted", w.Code)
	}
	policyUpdate(t, c, Object{"tags": []any{}, "rules": []Object{{"type": "ACTION_ACCEPT"}}, "capabilities": []any{}})
}
func TestManagementExportRedactsNativeJoinCredentials(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	_, _ = s.controller.PatchNetwork(context.Background(), policyNetwork, Object{"authTokens": Object{"join-secret": 0}, "ssoConfig": Object{"clientSecret": "sso-secret"}})
	_, _ = s.controller.PatchMember(context.Background(), policyNetwork, policyMember, Object{"lastAuthorizedCredential": "member-secret", "authenticationURL": "https://example.test/?token=hidden"})
	s.refresh(context.Background())
	w := c.request(t, "GET", "/api/export", nil)
	for _, secret := range []string{"join-secret", "sso-secret", "member-secret", "token=hidden"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("export leaked join credentials")
		}
	}
	if policyView(t, c)["authTokens"] == nil {
		t.Fatal("redaction mutated live snapshot")
	}
}
