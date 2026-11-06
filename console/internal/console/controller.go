package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Controller struct {
	Demo                      bool
	Address, TokenFile, Token string
	Store                     *Store
	client                    *http.Client
	mu                        sync.Mutex
	cacheMu                   sync.Mutex
	memberCache               map[string]cachedMember
}

type cachedMember struct {
	revision string
	at       time.Time
	value    Object
}

func NewController(demo bool, address, tokenFile, token string, store *Store) (*Controller, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
		return nil, fail("无效的控制器地址")
	}
	c := &Controller{Demo: demo, Address: strings.TrimRight(address, "/"), TokenFile: tokenFile, Token: token, Store: store, client: &http.Client{Timeout: 8 * time.Second}, memberCache: map[string]cachedMember{}}
	if demo {
		if err = c.seed(); err != nil {
			return nil, err
		}
	}
	return c, nil
}
func (c *Controller) call(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	token := c.Token
	if token == "" {
		data, err := os.ReadFile(c.TokenFile)
		if err != nil {
			return fail("无法读取 ZeroTier 凭据，请检查 systemd 凭据或令牌文件权限")
		}
		token = strings.TrimSpace(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Address+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("X-ZT1-Auth", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fail("ZeroTier 控制器连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ZeroTier 控制器返回 HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}
func (c *Controller) Status(ctx context.Context) (Object, error) {
	if c.Demo {
		return Object{"address": "f47cef1d94", "version": "1.16.2", "online": true, "mode": "demo"}, nil
	}
	var o Object
	err := c.call(ctx, "GET", "/status", nil, &o)
	return o, err
}
func (c *Controller) Networks(ctx context.Context) ([]Object, error) {
	list := []Object{}
	if c.Demo {
		keys, err := c.Store.RecordKeys("network:")
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			v, e := c.Store.Record(k)
			if e != nil {
				return nil, e
			}
			list = append(list, v)
		}
		return list, nil
	}
	var ids []string
	if err := c.call(ctx, "GET", "/controller/network", nil, &ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		n, err := c.Network(ctx, id)
		if err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return list, nil
}
func (c *Controller) Network(ctx context.Context, id string) (Object, error) {
	if c.Demo {
		return c.Store.Record("network:" + id)
	}
	var o Object
	err := c.call(ctx, "GET", "/controller/network/"+id, nil, &o)
	return o, err
}
func (c *Controller) Members(ctx context.Context, id string) ([]Object, error) {
	if c.Demo {
		keys, err := c.Store.RecordKeys("member:" + id + ":")
		if err != nil {
			return nil, err
		}
		list := []Object{}
		for _, key := range keys {
			m, e := c.Store.Record(key)
			if e != nil {
				return nil, e
			}
			list = append(list, m)
		}
		return list, nil
	}
	var revisions map[string]any
	if err := c.call(ctx, "GET", "/controller/network/"+id+"/member", nil, &revisions); err != nil {
		return nil, err
	}
	ids := []string{}
	for id := range revisions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	list := make([]Object, len(ids))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var first error
	var errMu sync.Mutex
	for i, idm := range ids {
		key := id + ":" + idm
		c.cacheMu.Lock()
		cached, ok := c.memberCache[key]
		c.cacheMu.Unlock()
		if ok && cached.revision == fmt.Sprint(revisions[idm]) && time.Since(cached.at) < 12*time.Second {
			list[i] = clone(cached.value)
			continue
		}
		wg.Add(1)
		go func(i int, idm string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			var m Object
			err := c.call(ctx, "GET", "/controller/network/"+id+"/member/"+idm, nil, &m)
			if err != nil {
				errMu.Lock()
				if first == nil {
					first = err
					cancel()
				}
				errMu.Unlock()
				return
			}
			list[i] = m
			c.cacheMu.Lock()
			c.memberCache[id+":"+idm] = cachedMember{fmt.Sprint(revisions[idm]), time.Now(), clone(m)}
			c.cacheMu.Unlock()
		}(i, idm)
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return list, nil
}
func (c *Controller) Member(ctx context.Context, nwid, id string) (Object, error) {
	if c.Demo {
		return c.Store.Record("member:" + nwid + ":" + id)
	}
	var m Object
	err := c.call(ctx, "GET", "/controller/network/"+nwid+"/member/"+id, nil, &m)
	return m, err
}
func (c *Controller) Peers(ctx context.Context) ([]Object, error) {
	if c.Demo {
		return []Object{}, nil
	}
	var peers []Object
	err := c.call(ctx, "GET", "/peer", nil, &peers)
	return peers, err
}
func (c *Controller) PatchNetwork(ctx context.Context, id string, patch Object) (Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Demo {
		n, err := c.Network(ctx, id)
		if err != nil {
			return nil, err
		}
		for k, v := range patch {
			n[k] = v
		}
		err = c.Store.PutRecord("network:"+id, n)
		return n, err
	}
	var n Object
	err := c.call(ctx, "POST", "/controller/network/"+id, patch, &n)
	return n, err
}
func (c *Controller) PatchMember(ctx context.Context, nwid, id string, patch Object) (Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Demo {
		m, err := c.Store.Record("member:" + nwid + ":" + id)
		if err != nil {
			return nil, err
		}
		for k, v := range patch {
			m[k] = v
		}
		err = c.Store.PutRecord("member:"+nwid+":"+id, m)
		return m, err
	}
	var m Object
	err := c.call(ctx, "POST", "/controller/network/"+nwid+"/member/"+id, patch, &m)
	c.cacheMu.Lock()
	delete(c.memberCache, nwid+":"+id)
	c.cacheMu.Unlock()
	return m, err
}
func (c *Controller) CreateNetwork(ctx context.Context, name string) (Object, error) {
	if c.Demo {
		id := "f47cef1d94" + randomHex(3)
		n := demoNetwork(id, name, "10.72.18")
		return n, c.Store.PutRecord("network:"+id, n)
	}
	status, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}
	var n Object
	err = c.call(ctx, "POST", "/controller/network/"+str(status["address"])+"______", Object{"name": name, "private": true}, &n)
	return n, err
}
func (c *Controller) Delete(ctx context.Context, nwid, member string) error {
	if c.Demo {
		if member != "" {
			return c.Store.DeleteRecord("member:" + nwid + ":" + member)
		}
		keys, err := c.Store.RecordKeys("member:" + nwid + ":")
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err = c.Store.DeleteRecord(k); err != nil {
				return err
			}
		}
		return c.Store.DeleteRecord("network:" + nwid)
	}
	path := "/controller/network/" + nwid
	if member != "" {
		path += "/member/" + member
	}
	return c.call(ctx, "DELETE", path, nil, nil)
}
func demoNetwork(id, name, subnet string) Object {
	return Object{"id": id, "nwid": id, "name": name, "private": true, "v4AssignMode": Object{"zt": true}, "v6AssignMode": Object{"zt": false, "rfc4193": false, "6plane": false}, "routes": []Object{{"target": subnet + ".0/24", "via": nil}}, "ipAssignmentPools": []Object{{"ipRangeStart": subnet + ".1", "ipRangeEnd": subnet + ".254"}}, "dns": Object{"domain": "", "servers": []string{}}, "enableBroadcast": true, "multicastLimit": 32, "mtu": 2800, "rules": []Object{{"type": "ACTION_ACCEPT"}}}
}
func (c *Controller) seed() error {
	if c.Store.Get("demo:seeded") != nil {
		return nil
	}
	nets := []Object{demoNetwork("f47cef1d94743881", "LovinYarn", "10.0.24"), demoNetwork("f47cef1d94fc97b1", "家庭实验室", "10.2.22"), demoNetwork("f47cef1d94a1f5ec", "远程办公", "172.26.18")}
	nets[0]["routes"] = []Object{{"target": "10.0.24.0/24", "via": nil}, {"target": "192.168.2.0/24", "via": "10.0.24.187"}, {"target": "192.168.11.0/24", "via": "10.0.24.49"}}
	nets[0]["dns"] = Object{"domain": "home.arpa", "servers": []string{"10.0.24.187"}}
	names := []string{"主路由 · OpenWrt", "家庭服务器 · Debian", "工作电脑 · Windows", "MacBook Pro", "Pixel 9 Pro", "客厅电视 · Android TV", "NAS · Synology", "备用节点 · N100", "iPad Pro", "远程网关 · OpenWrt", "测试设备 · Linux", "待授权设备"}
	ips := []int{187, 49, 221, 35, 148, 18, 3, 115, 72, 167, 234, 0}
	ids := []string{"0c1cb97eaa", "1df39c9d88", "1996777016", "0567262e8e", "22e14df1f6", "11b0589062", "11b771270b", "4e74ec9d41", "6464d51c56", "97978f5b0d", "49861e65b7", "c40f37aab9"}
	for j, n := range nets {
		id := str(n["nwid"])
		if err := c.Store.PutRecord("network:"+id, n); err != nil {
			return err
		}
		count := len(names)
		if j == 1 {
			count = 5
		}
		if j == 2 {
			count = 3
		}
		for i := 0; i < count; i++ {
			ipsList := []string{}
			if ips[i] != 0 {
				prefix := []string{"10.0.24", "10.2.22", "172.26.18"}[j]
				ipsList = append(ipsList, fmt.Sprintf("%s.%d", prefix, ips[i]))
			}
			m := Object{"id": ids[i], "address": ids[i], "authorized": i != 11, "activeBridge": false, "noAutoAssignIps": false, "ipAssignments": ipsList, "lastSeen": time.Now().Add(-time.Duration(i*7) * time.Minute).UnixMilli(), "_demoOnline": i < 8, "_demoLatency": []int{2, 4, 18, 23, 37, 45, 12, 31, 0, 0, 0, 0}[i], "_demoVersion": "1.16.2", "_demoPath": fmt.Sprintf("203.0.113.%d/9993", i+11)}
			if err := c.Store.PutRecord("member:"+id+":"+ids[i], m); err != nil {
				return err
			}
			if err := c.Store.Set(ids[i], names[i]); err != nil {
				return err
			}
		}
	}
	_ = c.Store.Set("network:f47cef1d94743881:description", "家庭设备与远程访问网络")
	_ = c.Store.Set("network:f47cef1d94743881:pinnedMembers", []string{ids[0], ids[1]})
	_ = c.Store.Set("tags:f47cef1d94743881:"+ids[0], []string{"网关"})
	return c.Store.Set("demo:seeded", true)
}
