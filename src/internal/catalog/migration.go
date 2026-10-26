package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	"hzk-doc/internal/content"
)

var legacyTrashID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[A-Za-z0-9_-]{6,32}$`)

func (s *Store) MigrateFilenames() (int, error) {
	if err := s.syncDocuments(true); err != nil {
		return 0, err
	}
	rows, err := s.db.Query("SELECT id,path,storage_path,trash_id FROM documents ORDER BY storage_path")
	if err != nil {
		return 0, err
	}
	var documents []Document
	for rows.Next() {
		var doc Document
		if err := rows.Scan(&doc.ID, &doc.Path, &doc.StoragePath, &doc.TrashID); err != nil {
			rows.Close()
			return 0, err
		}
		documents = append(documents, doc)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	entries, err := s.TrashList()
	if err != nil {
		return 0, err
	}
	var moves []move
	var updates []Document
	for _, doc := range documents {
		if _, err := s.root.Lstat(doc.StoragePath); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return 0, err
		}
		clean := path.Join(path.Dir(doc.Path), CleanName(path.Base(doc.Path)))
		if clean == doc.Path {
			continue
		}
		if !IsDocument(clean) || strings.TrimSuffix(path.Base(clean), path.Ext(clean)) == "" {
			return 0, fmt.Errorf("去除 ID 后文件名无效: %s", doc.Path)
		}
		nextStorage := clean
		if doc.TrashID != "" {
			nextStorage = payloadPath(doc.TrashID, clean)
			for i := range entries {
				if entries[i].ID == doc.TrashID {
					for j := range entries[i].Items {
						if entries[i].Items[j].Path == doc.Path {
							entries[i].Items[j].Path = clean
						}
					}
				}
			}
		}
		moves = append(moves, move{From: doc.StoragePath, To: nextStorage})
		updates = append(updates, doc)
	}
	if len(moves) == 0 {
		return 0, nil
	}
	err = s.applyMoves(moves, func(tx *sql.Tx) error {
		for i, doc := range updates {
			clean := path.Join(path.Dir(doc.Path), CleanName(path.Base(doc.Path)))
			if _, err := tx.Exec("INSERT INTO aliases(path,document_id) VALUES(?,?) ON CONFLICT(path) DO UPDATE SET document_id=excluded.document_id", doc.Path, doc.ID); err != nil {
				return err
			}
			if _, err := tx.Exec("UPDATE documents SET path=?,storage_path=? WHERE id=?", clean, moves[i].To, doc.ID); err != nil {
				return err
			}
		}
		for _, entry := range entries {
			items, err := json.Marshal(entry.Items)
			if err != nil {
				return err
			}
			if _, err := tx.Exec("UPDATE trash SET items=? WHERE id=?", string(items), entry.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(moves), nil
}

func (s *Store) importLegacyTrash() error {
	var marker string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key='legacy_trash_imported'").Scan(&marker)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	base := content.TrashDir + "/entries"
	if err := s.safePath(base); err != nil {
		return err
	}
	entries, err := fs.ReadDir(s.root.FS(), base)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, dir := range entries {
		if !dir.IsDir() || !legacyTrashID.MatchString(dir.Name()) {
			continue
		}
		id := dir.Name()
		metaPath := TrashPath(id) + "/meta.json"
		if err := s.safePath(metaPath); err != nil {
			return err
		}
		data, err := s.root.ReadFile(metaPath)
		if err != nil {
			return err
		}
		var entry Trash
		if err := json.Unmarshal(data, &entry); err != nil || len(entry.Items) == 0 {
			return fmt.Errorf("旧回收站条目无效: %s", id)
		}
		for _, item := range entry.Items {
			if content.ValidatePath(item.Path, false) != nil || (item.Type != "file" && item.Type != "folder") {
				return fmt.Errorf("旧回收站路径无效: %s", id)
			}
			base := payloadPath(id, item.Path)
			if err := s.safePath(base); err != nil {
				return err
			}
			if err := fs.WalkDir(s.root.FS(), base, func(name string, entryFile fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				relative := strings.TrimPrefix(name, TrashPath(id)+"/files/")
				if !entryFile.Type().IsRegular() || !IsDocument(relative) {
					return nil
				}
				docID := NewID()
				if match := legacyID.FindStringSubmatch(strings.TrimSuffix(path.Base(relative), path.Ext(relative))); match != nil {
					docID = strings.ToLower(match[1])
				}
				date := entry.DeletedAt.UTC().Format(time.RFC3339Nano)
				_, err = tx.Exec("INSERT INTO documents(id,path,storage_path,created_at,updated_at,deleted_at,trash_id) VALUES(?,?,?,?,?,?,?)", docID, relative, name, date, date, date, id)
				return err
			}); err != nil {
				return err
			}
		}
		items, err := json.Marshal(entry.Items)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO trash(id,deleted_at,items) VALUES(?,?,?)", id, entry.DeletedAt.UTC().Format(time.RFC3339Nano), string(items)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO metadata(key,value) VALUES('legacy_trash_imported','1')"); err != nil {
		return err
	}
	return tx.Commit()
}
