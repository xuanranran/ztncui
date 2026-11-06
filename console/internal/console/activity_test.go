package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestLiveSnapshotUsesNativePeerActivity(t *testing.T) {
	now := time.Now().UnixMilli()
	ids := []string{"0000000001", "0000000002", "0000000003", "0000000004", "0000000005", "0000000006", "0000000007"}
	revisions := Object{}
	for _, id := range ids {
		revisions[id] = 1
	}
	peer := func(id string, received int64, active bool) Object {
		return Object{"address": id, "version": "1.16.2", "latency": 4, "paths": []Object{{"address": "192.0.2.1/9993", "lastReceive": received, "active": active, "expired": !active}}}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ZT1-Auth") != "test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/status":
			writeJSON(w, 200, Object{"address": ids[3], "online": true, "version": "1.16.2"})
		case "/peer":
			writeJSON(w, 200, []Object{peer(ids[0], now-1000, true), peer(ids[1], now-300000, false), peer(ids[2], now-2000, false), peer(ids[5], now+3600000, true), {"address": ids[6], "version": "1.16.2", "paths": []Object{}}})
		case "/controller/network":
			writeJSON(w, 200, []string{"0000000004abcdef"})
		case "/controller/network/0000000004abcdef":
			writeJSON(w, 200, Object{"nwid": "0000000004abcdef", "name": "native"})
		case "/controller/network/0000000004abcdef/member":
			writeJSON(w, 200, revisions)
		default:
			id := strings.TrimPrefix(r.URL.Path, "/controller/network/0000000004abcdef/member/")
			if revisions[id] == nil {
				http.NotFound(w, r)
				return
			}
			member := Object{"id": id, "authorized": true}
			if id == ids[4] {
				member["lastSeen"] = now - 3000
			}
			writeJSON(w, 200, member)
		}
	}))
	defer upstream.Close()
	s, err := New(Config{Database: filepath.Join(t.TempDir(), "live.sqlite"), Address: upstream.URL, Token: "test-token"}, fstest.MapFS{"index.html": {Data: []byte("test")}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.refresh(context.Background())
	networks := objects(s.current()["networks"])
	if len(networks) != 1 || number(networks[0]["online"]) != 4 {
		t.Fatalf("unexpected live snapshot: %#v", s.current())
	}
	members := objects(networks[0]["members"])
	wantOnline := []bool{true, false, true, true, true, false, false}
	wantConnection := []string{"direct", "unknown", "relay", "unknown", "unknown", "unknown", "unknown"}
	for index, member := range members {
		if flag(member["online"]) != wantOnline[index] || str(member["connection"]) != wantConnection[index] {
			t.Errorf("member %s has unexpected activity: %#v", ids[index], member)
		}
		if index < 3 && number(member["lastSeen"]) == 0 {
			t.Errorf("member %s lost peer receive time", ids[index])
		}
	}
}
