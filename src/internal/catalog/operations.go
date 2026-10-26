package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"hzk-doc/internal/content"
)

type move struct {
	From string
	To   string
}

type operation struct {
	Moves       []move
	Directories []string
	Purge       []string
	Cleanup     []string
}

func TrashPath(id string) string {
	return content.TrashDir + "/entries/" + id
}

func payloadPath(id, name string) string {
	return TrashPath(id) + "/files/" + name
}

func within(name, parent string) bool {
	return name == parent || strings.HasPrefix(name, parent+"/")
}

func (s *Store) pending(op operation) error {
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO pending_operation(id,payload) VALUES(1,?)", string(data))
	return err
}

func (s *Store) recover() error {
	var raw string
	err := s.db.QueryRow("SELECT payload FROM pending_operation WHERE id=1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var op operation
	if err := json.Unmarshal([]byte(raw), &op); err != nil {
		return err
	}
	if len(op.Purge) > 0 {
		return s.finishPurge(op.Purge, op.Cleanup)
	}
	return s.rollback(op)
}

func (s *Store) rollback(op operation) error {
	for i := len(op.Moves) - 1; i >= 0; i-- {
		m := op.Moves[i]
		if err := s.safePath(m.From); err != nil {
			return err
		}
		if err := s.safePath(m.To); err != nil {
			return err
		}
		_, fromErr := s.root.Lstat(m.From)
		_, toErr := s.root.Lstat(m.To)
		switch {
		case fromErr == nil && errors.Is(toErr, fs.ErrNotExist):
			continue
		case errors.Is(fromErr, fs.ErrNotExist) && toErr == nil:
			if err := s.root.Rename(m.To, m.From); err != nil {
				return err
			}
		default:
			return fmt.Errorf("无法安全恢复文件移动 %s → %s，已保留操作日志", m.From, m.To)
		}
	}
	for i := len(op.Directories) - 1; i >= 0; i-- {
		// 只移除本次操作创建的空目录，不删除任何恢复数据。
		_ = s.root.Remove(op.Directories[i])
	}
	_, err := s.db.Exec("DELETE FROM pending_operation WHERE id=1")
	return err
}

func (s *Store) applyMoves(moves []move, commit func(*sql.Tx) error) error {
	if err := s.recover(); err != nil {
		return err
	}
	op := operation{Moves: moves}
	seenDirs := make(map[string]bool)
	for i, m := range moves {
		if m.From == m.To {
			return ErrConflict
		}
		for _, other := range moves[:i] {
			if within(m.To, other.To) || within(other.To, m.To) || within(m.From, other.From) || within(other.From, m.From) {
				return ErrConflict
			}
		}
		if err := s.safePath(m.From); err != nil {
			return err
		}
		if err := s.safePath(m.To); err != nil {
			return err
		}
		if _, err := s.root.Lstat(m.From); err != nil {
			return err
		}
		if _, err := s.root.Lstat(m.To); !errors.Is(err, fs.ErrNotExist) {
			if err == nil {
				return ErrConflict
			}
			return err
		}
		for dir := path.Dir(m.To); dir != "."; dir = path.Dir(dir) {
			if _, err := s.root.Lstat(dir); err == nil {
				break
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if !seenDirs[dir] {
				seenDirs[dir] = true
				op.Directories = append(op.Directories, dir)
			}
		}
	}
	sort.Slice(op.Directories, func(i, j int) bool { return len(op.Directories[i]) < len(op.Directories[j]) })
	if err := s.pending(op); err != nil {
		return err
	}
	fail := func(err error) error { return errors.Join(err, s.rollback(op)) }
	for _, m := range moves {
		if err := s.root.MkdirAll(path.Dir(m.To), 0o755); err != nil {
			return fail(err)
		}
		if _, err := s.root.Lstat(m.To); !errors.Is(err, fs.ErrNotExist) {
			return fail(ErrConflict)
		}
		if err := s.root.Rename(m.From, m.To); err != nil {
			return fail(err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fail(err)
	}
	if err = commit(tx); err == nil {
		_, err = tx.Exec("DELETE FROM pending_operation WHERE id=1")
	}
	if err != nil {
		tx.Rollback()
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		// COMMIT 的结果可能不明确，依据持久日志决定是否撤回文件移动。
		return errors.Join(err, s.recover())
	}
	return nil
}

func (s *Store) Delete(entry Trash, retained map[string]bool) error {
	active, err := s.Active()
	if err != nil {
		return err
	}
	moves := make([]move, 0, len(entry.Items))
	for _, item := range entry.Items {
		if item.Merge {
			nested, err := s.deleteFolderMoves(item.Path, payloadPath(entry.ID, item.Path), retained)
			if err != nil {
				return err
			}
			moves = append(moves, nested...)
		} else {
			moves = append(moves, move{From: item.Path, To: payloadPath(entry.ID, item.Path)})
		}
	}
	if len(moves) == 0 {
		return ErrConflict
	}
	items, err := json.Marshal(entry.Items)
	if err != nil {
		return err
	}
	date := entry.DeletedAt.UTC().Format(time.RFC3339Nano)
	return s.applyMoves(moves, func(tx *sql.Tx) error {
		for name, doc := range active {
			for _, item := range entry.Items {
				if within(name, item.Path) && !retained[name] {
					if _, err := tx.Exec("UPDATE documents SET storage_path=?,deleted_at=?,trash_id=?,updated_at=? WHERE id=?", payloadPath(entry.ID, name), date, entry.ID, date, doc.ID); err != nil {
						return err
					}
					break
				}
			}
		}
		_, err := tx.Exec("INSERT INTO trash(id,deleted_at,items) VALUES(?,?,?)", entry.ID, date, string(items))
		return err
	})
}

func (s *Store) Restore(entries []Trash) error {
	var moves []move
	for _, entry := range entries {
		for _, item := range entry.Items {
			if item.Merge {
				nested, err := s.restoreFolderMoves(payloadPath(entry.ID, item.Path), item.Path)
				if err != nil {
					return err
				}
				moves = append(moves, nested...)
			} else {
				moves = append(moves, move{From: payloadPath(entry.ID, item.Path), To: item.Path})
			}
		}
	}
	err := s.applyMoves(moves, func(tx *sql.Tx) error {
		for _, entry := range entries {
			if _, err := tx.Exec("UPDATE documents SET storage_path=path,deleted_at=NULL,trash_id='',updated_at=? WHERE trash_id=?", time.Now().UTC().Format(time.RFC3339Nano), entry.ID); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM trash WHERE id=?", entry.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, entry := range entries {
		// 文件均已还原，仅清理回收站容器；条目状态由数据库决定。
		_ = s.root.RemoveAll(TrashPath(entry.ID))
	}
	return nil
}

func (s *Store) Purge(entries []Trash, extras ...Item) error {
	if len(entries) == 0 {
		return nil
	}
	if err := s.recover(); err != nil {
		return err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	cleanup := make([]string, 0, len(extras))
	for _, item := range extras {
		cleanup = append(cleanup, item.Path)
	}
	if err := s.pending(operation{Purge: ids, Cleanup: cleanup}); err != nil {
		return err
	}
	return s.finishPurge(ids, cleanup)
}

func (s *Store) finishPurge(ids, cleanup []string) error {
	for _, name := range cleanup {
		if err := content.ValidatePath(name, false); err != nil {
			return err
		}
		if err := s.safePath(name); err != nil {
			return err
		}
		if err := s.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, id := range ids {
		if strings.ContainsAny(id, "/\\.") || id == "" {
			return errors.New("回收站标识无效")
		}
		name := TrashPath(id)
		if err := s.safePath(name); err != nil {
			return err
		}
		if err := s.root.RemoveAll(name); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range cleanup {
		if _, err := tx.Exec("DELETE FROM documents WHERE path=? AND deleted_at IS NULL", name); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if _, err := tx.Exec("DELETE FROM documents WHERE trash_id=? AND deleted_at IS NOT NULL", id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM trash WHERE id=?", id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM pending_operation WHERE id=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Rename(from, to string) error {
	return s.renameFiles([]move{{From: from, To: to}})
}

func (s *Store) renameFiles(moves []move) error {
	active, err := s.Active()
	if err != nil {
		return err
	}
	return s.applyMoves(moves, func(tx *sql.Tx) error {
		for _, m := range moves {
			for old, doc := range active {
				if !within(old, m.From) {
					continue
				}
				next := m.To + strings.TrimPrefix(old, m.From)
				if _, err := tx.Exec("INSERT INTO aliases(path,document_id) VALUES(?,?) ON CONFLICT(path) DO UPDATE SET document_id=excluded.document_id", old, doc.ID); err != nil {
					return err
				}
				if _, err := tx.Exec("UPDATE documents SET path=?,storage_path=?,updated_at=? WHERE id=?", next, next, time.Now().UTC().Format(time.RFC3339Nano), doc.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
