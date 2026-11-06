package console

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func HashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, 3, 32*1024, 1, 32)
	enc := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=19$m=32768,t=3,p=1$%s$%s", enc(salt), enc(key))
}
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[2] != "v=19" {
		return false
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false
	}
	values := []uint64{}
	for i, p := range params {
		kv := strings.Split(p, "=")
		if len(kv) != 2 || kv[0] != []string{"m", "t", "p"}[i] {
			return false
		}
		n, e := strconv.ParseUint(kv[1], 10, 32)
		if e != nil {
			return false
		}
		values = append(values, n)
	}
	if values[0] < 8 || values[0] > 256*1024 || values[1] < 1 || values[1] > 10 || values[2] < 1 || values[2] > 16 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[4])
	if e != nil {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(parts[5])
	if e != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	var key []byte
	if parts[1] == "argon2i" {
		key = argon2.Key([]byte(password), salt, uint32(values[1]), uint32(values[0]), uint8(values[2]), uint32(len(expected)))
	} else if parts[1] == "argon2id" {
		key = argon2.IDKey([]byte(password), salt, uint32(values[1]), uint32(values[0]), uint8(values[2]), uint32(len(expected)))
	} else {
		return false
	}
	return subtle.ConstantTimeCompare(expected, key) == 1
}

type session struct {
	CSRF    string
	Expires time.Time
}
type loginAttempt struct {
	Count int
	Reset time.Time
}
type Auth struct {
	mu       sync.Mutex
	sessions map[string]session
	attempts map[string]loginAttempt
}

func newAuth() *Auth {
	return &Auth{sessions: map[string]session{}, attempts: map[string]loginAttempt{}}
}
func (a *Auth) get(r *http.Request) (session, bool) {
	c, err := r.Cookie("ztncui_session")
	if err != nil {
		return session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if ok && time.Now().After(s.Expires) {
		delete(a.sessions, c.Value)
		return session{}, false
	}
	return s, ok
}
func (a *Auth) issue(w http.ResponseWriter, r *http.Request) session {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for id, s := range a.sessions {
		if now.After(s.Expires) {
			delete(a.sessions, id)
		}
	}
	id := randomHex(32)
	s := session{randomHex(32), now.Add(12 * time.Hour)}
	a.sessions[id] = s
	http.SetCookie(w, &http.Cookie{Name: "ztncui_session", Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	return s
}
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, e := r.Cookie("ztncui_session"); e == nil {
		delete(a.sessions, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "ztncui_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
func (a *Auth) allowed(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, v := range a.attempts {
		if now.After(v.Reset) {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[ip]
	if v.Reset.IsZero() {
		v.Reset = now.Add(10 * time.Minute)
	}
	v.Count++
	a.attempts[ip] = v
	return v.Count <= 8
}
