package catalog

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hzk-doc/internal/content"

	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

const DatabaseFile = ".hzk-doc.sqlite"

var legacyID = regexp.MustCompile(`(?i)_([0-9]{8}-[a-z]{6})$`)
var ErrConflict = errors.New("目标已存在")

// 同一个进程中的管理操作与目录刷新共用这把锁。
type Store struct {
	sync.Mutex
	db   *sql.DB
	root *os.Root
	lock *flock.Flock
}

type Document struct {
	ID          string
	Path        string
	StoragePath string
	DeletedAt   string
	TrashID     string
}

type Item struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Merge bool   `json:"merge,omitempty"`
}

type Trash struct {
	ID        string    `json:"id"`
	DeletedAt time.Time `json:"deletedAt"`
	Items     []Item    `json:"items"`
	Size      int64     `json:"size"`
}

func Open(directory string) (*Store, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root}
	for _, name := range []string{DatabaseFile, DatabaseFile + "-wal", DatabaseFile + "-shm", DatabaseFile + "-journal", ".hzk-doc.lock"} {
		if err := s.safePath(name); err != nil {
			root.Close()
			return nil, err
		}
	}
	s.lock = flock.New(filepath.Join(absolute, ".hzk-doc.lock"))
	locked, err := s.lock.TryLock()
	if err != nil || !locked {
		root.Close()
		s.lock.Close()
		return nil, errors.Join(errors.New("文档库已被其他进程使用或无法加锁"), err)
	}
	db, err := sql.Open("sqlite", filepath.Join(absolute, DatabaseFile))
	if err != nil {
		s.lock.Close()
		root.Close()
		return nil, err
	}
	s.db = db
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS documents (
 id TEXT PRIMARY KEY,
 path TEXT NOT NULL,
 storage_path TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 deleted_at TEXT,
 trash_id TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS documents_active_path ON documents(path) WHERE deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS aliases (
 path TEXT PRIMARY KEY,
 document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS trash (
 id TEXT PRIMARY KEY,
 deleted_at TEXT NOT NULL,
 items TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS pending_operation (
 id INTEGER PRIMARY KEY CHECK(id=1),
 payload TEXT NOT NULL
);`)
	if err == nil {
		err = s.recover()
	}
	if err == nil {
		err = s.importLegacyTrash()
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Recover() error {
	return s.recover()
}

func (s *Store) Close() error {
	return errors.Join(s.db.Close(), s.root.Close(), s.lock.Close())
}

func ValidSiteTheme(theme string) bool {
	return theme == "classic" || theme == "forest" || theme == "mono"
}

func (s *Store) SiteTheme() (string, error) {
	var theme string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key='site_theme'").Scan(&theme)
	if errors.Is(err, sql.ErrNoRows) {
		return "classic", nil
	}
	if err != nil {
		return "", err
	}
	if !ValidSiteTheme(theme) {
		return "classic", nil
	}
	return theme, nil
}

func (s *Store) SetSiteTheme(theme string) error {
	if !ValidSiteTheme(theme) {
		return errors.New("主题无效")
	}
	_, err := s.db.Exec(`INSERT INTO metadata(key,value) VALUES('site_theme',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, theme)
	return err
}

func NewID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}

func CleanName(name string) string {
	ext := path.Ext(name)
	return legacyID.ReplaceAllString(strings.TrimSuffix(name, ext), "") + ext
}

func (s *Store) safePath(name string) error {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\\x00:") {
		return errors.New("路径无效")
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := s.root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return errors.New("不允许符号链接")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return errors.New("父路径不是目录")
		}
	}
	return nil
}

func IsDocument(name string) bool {
	if content.Kind(name) == "" || content.ValidatePath(name, false) != nil {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.EqualFold(part, "assets") {
			return false
		}
	}
	return true
}

func (s *Store) Sync() error {
	return s.syncDocuments(false)
}

func (s *Store) syncDocuments(importLegacy bool) error {
	var names []string
	err := fs.WalkDir(s.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".") || strings.EqualFold(entry.Name(), "assets")) {
			return fs.SkipDir
		}
		if entry.Type().IsRegular() && IsDocument(name) {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.register(names, importLegacy)
}

func (s *Store) Register(names []string) error {
	return s.register(names, false)
}

func (s *Store) register(names []string, importLegacy bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range names {
		var id string
		err := tx.QueryRow("SELECT id FROM documents WHERE path=? AND deleted_at IS NULL", name).Scan(&id)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id = NewID()
		stem := strings.TrimSuffix(path.Base(name), path.Ext(name))
		if match := legacyID.FindStringSubmatch(stem); importLegacy && match != nil {
			id = strings.ToLower(match[1])
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec("INSERT INTO documents(id,path,storage_path,created_at,updated_at) VALUES(?,?,?,?,?)", id, name, name, now, now); err != nil {
			return fmt.Errorf("登记文档 %s 失败: %w", name, err)
		}
	}
	return tx.Commit()
}

func (s *Store) Active() (map[string]Document, error) {
	rows, err := s.db.Query("SELECT id,path,storage_path FROM documents WHERE deleted_at IS NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]Document)
	for rows.Next() {
		var doc Document
		if err := rows.Scan(&doc.ID, &doc.Path, &doc.StoragePath); err != nil {
			return nil, err
		}
		result[doc.Path] = doc
	}
	return result, rows.Err()
}

func (s *Store) Touch(name string) error {
	_, err := s.db.Exec("UPDATE documents SET updated_at=? WHERE path=? AND deleted_at IS NULL", time.Now().UTC().Format(time.RFC3339Nano), name)
	return err
}

func (s *Store) ResolveOrder(order map[string][]string) {
	for dir, names := range order {
		for i, name := range names {
			if doc, err := s.FindPath(path.Join(dir, name)); err == nil && path.Dir(doc.Path) == path.Dir(path.Join(dir, name)) {
				names[i] = path.Base(doc.Path)
			}
		}
	}
}

func (s *Store) FindPath(name string) (Document, error) {
	var doc Document
	err := s.db.QueryRow(`SELECT id,path,storage_path FROM documents WHERE deleted_at IS NULL AND
(path=? OR id=(SELECT document_id FROM aliases WHERE path=?)) ORDER BY (path=?) DESC LIMIT 1`, name, name, name).Scan(&doc.ID, &doc.Path, &doc.StoragePath)
	if errors.Is(err, sql.ErrNoRows) {
		return doc, fs.ErrNotExist
	}
	return doc, err
}

func (s *Store) GetTrash(id string) (Trash, error) {
	var entry Trash
	var date, items string
	err := s.db.QueryRow("SELECT id,deleted_at,items FROM trash WHERE id=?", id).Scan(&entry.ID, &date, &items)
	if errors.Is(err, sql.ErrNoRows) {
		return entry, fs.ErrNotExist
	}
	if err != nil {
		return entry, err
	}
	entry.DeletedAt, err = time.Parse(time.RFC3339Nano, date)
	if err != nil {
		return entry, err
	}
	if err := json.Unmarshal([]byte(items), &entry.Items); err != nil {
		return entry, err
	}
	for _, item := range entry.Items {
		entry.Size += item.Size
	}
	return entry, nil
}

func (s *Store) TrashList() ([]Trash, error) {
	rows, err := s.db.Query("SELECT id FROM trash ORDER BY deleted_at DESC,id DESC")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	entries := make([]Trash, 0, len(ids))
	for _, id := range ids {
		entry, err := s.GetTrash(id)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
