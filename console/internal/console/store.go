package console

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
	CREATE TABLE IF NOT EXISTS admin (id INTEGER PRIMARY KEY CHECK(id=1), username TEXT NOT NULL UNIQUE, hash TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS records (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS audit (id INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL, action TEXT NOT NULL, target TEXT NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0600)
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Get(key string) any {
	var raw string
	if s.db.QueryRow("SELECT value FROM metadata WHERE key=?", key).Scan(&raw) != nil {
		return nil
	}
	var v any
	_ = json.Unmarshal([]byte(raw), &v)
	return v
}
func (s *Store) Set(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(b))
	return err
}
func (s *Store) Record(key string) (Object, error) {
	var raw string
	err := s.db.QueryRow("SELECT value FROM records WHERE key=?", key).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var v Object
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (s *Store) PutRecord(key string, v Object) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO records(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(b))
	return err
}
func (s *Store) RecordKeys(prefix string) ([]string, error) {
	rows, err := s.db.Query("SELECT key FROM records WHERE key LIKE ? ORDER BY key", prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var k string
		if err = rows.Scan(&k); err != nil {
			return nil, err
		}
		result = append(result, k)
	}
	return result, rows.Err()
}
func (s *Store) DeleteRecord(key string) error {
	_, err := s.db.Exec("DELETE FROM records WHERE key=?", key)
	return err
}
func (s *Store) Admin() (string, string, error) {
	var name, hash string
	err := s.db.QueryRow("SELECT username,hash FROM admin WHERE id=1").Scan(&name, &hash)
	return name, hash, err
}
func (s *Store) CreateAdmin(name, hash string) error {
	_, err := s.db.Exec("INSERT INTO admin(id,username,hash) VALUES(1,?,?)", name, hash)
	return err
}
func (s *Store) Password(hash string) error {
	_, err := s.db.Exec("UPDATE admin SET hash=? WHERE id=1", hash)
	return err
}
func (s *Store) Log(action, target string) {
	_, _ = s.db.Exec("INSERT INTO audit(at,action,target) VALUES(?,?,?)", time.Now().UTC().Format(time.RFC3339), action, target)
}
func (s *Store) Logs() ([]Object, error) {
	rows, err := s.db.Query("SELECT id,at,action,target FROM audit ORDER BY id DESC LIMIT 150")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Object{}
	for rows.Next() {
		var id int
		var at, action, target string
		if err = rows.Scan(&id, &at, &action, &target); err != nil {
			return nil, err
		}
		list = append(list, Object{"id": id, "at": at, "action": action, "target": target})
	}
	return list, rows.Err()
}

// Legacy files are read once into a transaction. The original directory is never written.
func (s *Store) ImportLegacy(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if s.Get("legacy:imported:"+absolute) != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(absolute, "passwd"))
	if err != nil {
		return fmt.Errorf("读取旧版管理员: %w", err)
	}
	var users map[string]struct {
		Name string `json:"name"`
		Hash string `json:"hash"`
	}
	if err = json.Unmarshal(data, &users); err != nil {
		return err
	}
	admin, ok := users["admin"]
	if !ok || admin.Hash == "" {
		return fail("旧版目录缺少 admin 账号")
	}
	entries, err := os.ReadDir(filepath.Join(absolute, "storage"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	values := map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(absolute, "storage", entry.Name()))
		if e != nil {
			return e
		}
		var item struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if e = json.Unmarshal(raw, &item); e != nil || item.Key == "" {
			return fmt.Errorf("无法解析旧存储 %s", entry.Name())
		}
		values[item.Key] = item.Value
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO admin(id,username,hash) VALUES(1,'admin',?) ON CONFLICT(id) DO NOTHING", admin.Hash); err != nil {
		return err
	}
	for key, value := range values {
		raw, _ := json.Marshal(value)
		if _, err = tx.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING", key, string(raw)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO metadata(key,value) VALUES(?,?)", "legacy:imported:"+absolute, `true`); err != nil {
		return err
	}
	return tx.Commit()
}
