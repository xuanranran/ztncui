package console

import "testing"

func TestDeviceIconIsLocalMetadata(t *testing.T) {
	s := testServer(t)
	c := &testClient{s: s}
	c.login(t)
	nwid, id := "f47cef1d94743881", "0c1cb97eaa"
	before, err := s.controller.Member(t.Context(), nwid, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, icon := range []string{"openwrt", "windows", "auto"} {
		w := c.request(t, "PATCH", "/api/networks/"+nwid+"/members/"+id, Object{"icon": icon})
		if w.Code != 200 {
			t.Fatalf("icon save: %d %s", w.Code, w.Body)
		}
		network := objectResponse(t, c.request(t, "GET", "/api/networks/"+nwid, nil))
		found := false
		for _, member := range objects(network["members"]) {
			if str(member["id"]) == id {
				found = str(member["icon"]) == icon
			}
		}
		if !found {
			t.Fatal("saved icon missing")
		}
	}
	after, err := s.controller.Member(t.Context(), nwid, id)
	if err != nil || digest(before) != digest(after) {
		t.Fatal("icon altered controller configuration")
	}
	for _, icon := range []any{"", "github", "auto|computer", "<svg>", 12, nil} {
		if w := c.request(t, "PATCH", "/api/networks/"+nwid+"/members/"+id, Object{"icon": icon}); w.Code != 400 {
			t.Fatal("invalid icon accepted")
		}
	}
}
