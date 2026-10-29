package catalog

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func put(t *testing.T, dir, name, data string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireFile(t *testing.T, dir, name, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v", name, data, err)
	}
}

func TestAssetDirectoryCaseVariantsAreNotDocuments(t *testing.T) {
	s, dir := testStore(t)
	for _, name := range []string{"Assets/note.md", "assets/image.png", "nested/ASSETS/doc.html"} {
		if IsDocument(name) {
			t.Fatalf("素材目录被识别为文档: %s", name)
		}
		put(t, dir, name, "asset")
	}
	put(t, dir, "notes/doc.md", "doc")
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	active, err := s.Active()
	if err != nil || len(active) != 1 || active["notes/doc.md"].ID == "" {
		t.Fatalf("登记了素材目录内文件: %+v %v", active, err)
	}
}

func TestIdentityLifecycle(t *testing.T) {
	s, dir := testStore(t)
	put(t, dir, "old.md", "original")
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	original, _ := s.FindPath("old.md")
	if original.ID == "" {
		t.Fatal("缺少 ID")
	}
	if err := s.Rename("old.md", "new.md"); err != nil {
		t.Fatal(err)
	}
	changed, _ := s.FindPath("new.md")
	if changed.ID != original.ID {
		t.Fatal("改名改变了 ID")
	}
	entry := Trash{ID: NewID(), DeletedAt: time.Now().UTC(), Items: []Item{{Path: "new.md", Type: "file", Size: 8}}}
	if err := s.Delete(entry, nil); err != nil {
		t.Fatal(err)
	}
	var deletedAt, storage string
	if err := s.db.QueryRow("SELECT deleted_at,storage_path FROM documents WHERE id=?", original.ID).Scan(&deletedAt, &storage); err != nil || deletedAt == "" || storage == "new.md" {
		t.Fatalf("软删除记录不完整: %s %s %v", deletedAt, storage, err)
	}
	put(t, dir, "new.md", "replacement")
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	replacement, _ := s.FindPath("new.md")
	if replacement.ID == original.ID {
		t.Fatal("同名新文档复用了旧 ID")
	}
	if err := s.Restore([]Trash{entry}); !errors.Is(err, ErrConflict) {
		t.Fatalf("应拒绝还原冲突: %v", err)
	}
	requireFile(t, dir, "new.md", "replacement")
	second := Trash{ID: NewID(), DeletedAt: time.Now().UTC(), Items: []Item{{Path: "new.md", Type: "file"}}}
	if err := s.Delete(second, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore([]Trash{entry}); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.FindPath("new.md")
	if restored.ID != original.ID {
		t.Fatal("还原改变了 ID")
	}
	if err := s.Purge([]Trash{second}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM documents WHERE id=?", replacement.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("彻底删除仍留记录: %d %v", count, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.FindPath("new.md")
	if err != nil || persisted.ID != original.ID {
		t.Fatalf("重启丢失 ID: %+v %v", persisted, err)
	}
}

func TestMoveRollbackOnDatabaseFailure(t *testing.T) {
	s, dir := testStore(t)
	put(t, dir, "a.md", "a")
	put(t, dir, "b.md", "b")
	err := s.applyMoves([]move{{"a.md", "deep/a.md"}, {"b.md", "deep/b.md"}}, func(tx *sql.Tx) error {
		return errors.New("模拟提交前失败")
	})
	if err == nil {
		t.Fatal("未返回失败")
	}
	requireFile(t, dir, "a.md", "a")
	requireFile(t, dir, "b.md", "b")
	if _, err := os.Stat(filepath.Join(dir, "deep")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("回滚留下目标目录: %v", err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM pending_operation").Scan(&count); err != nil || count != 0 {
		t.Fatalf("回滚未完成: %d %v", count, err)
	}
}

func TestInterruptedMoveRecoversAtStartup(t *testing.T) {
	s, dir := testStore(t)
	put(t, dir, "a.md", "a")
	put(t, dir, "b.md", "b")
	op := operation{Moves: []move{{"a.md", "a-moved.md"}, {"b.md", "b-moved.md"}}}
	if err := s.pending(op); err != nil {
		t.Fatal(err)
	}
	if err := s.root.Rename("a.md", "a-moved.md"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	requireFile(t, dir, "a.md", "a")
	requireFile(t, dir, "b.md", "b")
}

func TestBatchRestoreConflictsBeforeMoving(t *testing.T) {
	s, dir := testStore(t)
	var entries []Trash
	for _, text := range []string{"first", "second"} {
		put(t, dir, "same.md", text)
		if err := s.Sync(); err != nil {
			t.Fatal(err)
		}
		entry := Trash{ID: NewID(), DeletedAt: time.Now().UTC(), Items: []Item{{Path: "same.md", Type: "file"}}}
		if err := s.Delete(entry, nil); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if err := s.Restore(entries); !errors.Is(err, ErrConflict) {
		t.Fatalf("批量冲突未拒绝: %v", err)
	}
	for i, text := range []string{"first", "second"} {
		requireFile(t, dir, payloadPath(entries[i].ID, "same.md"), text)
	}
}

func TestRestorePermissionFailureCanRetry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不适用权限拒绝用例")
	}
	s, dir := testStore(t)
	put(t, dir, "sub/doc.md", "doc")
	put(t, dir, "assets/a.png", "image")
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	entry := Trash{ID: NewID(), DeletedAt: time.Now().UTC(), Items: []Item{{Path: "sub", Type: "folder"}, {Path: "assets/a.png", Type: "file"}}}
	if err := s.Delete(entry, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "assets"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "assets"), 0o755) })
	if err := s.Restore([]Trash{entry}); err == nil {
		t.Fatal("预期权限失败")
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("还原失败未撤回已移动的目录")
	}
	requireFile(t, dir, payloadPath(entry.ID, "sub/doc.md"), "doc")
	requireFile(t, dir, payloadPath(entry.ID, "assets/a.png"), "image")
	if err := os.Chmod(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore([]Trash{entry}); err != nil {
		t.Fatal(err)
	}
	requireFile(t, dir, "sub/doc.md", "doc")
	requireFile(t, dir, "assets/a.png", "image")
}

func TestMigrationKeepsIDAndAliases(t *testing.T) {
	s, dir := testStore(t)
	put(t, dir, "notes/intro_20260101-abcdef.md", "content")
	put(t, dir, "plain.md", "plain")
	count, err := s.MigrateFilenames()
	if err != nil || count != 1 {
		t.Fatalf("迁移: %d %v", count, err)
	}
	doc, err := s.FindPath("notes/intro.md")
	if err != nil || doc.ID != "20260101-abcdef" {
		t.Fatalf("迁移 ID: %+v %v", doc, err)
	}
	alias, err := s.FindPath("notes/intro_20260101-abcdef.md")
	if err != nil || alias.ID != doc.ID || alias.Path != doc.Path {
		t.Fatalf("旧引用无法定位: %+v %v", alias, err)
	}
	requireFile(t, dir, "notes/intro.md", "content")
	order := map[string][]string{"notes": {"intro_20260101-abcdef.md"}}
	s.ResolveOrder(order)
	if order["notes"][0] != "intro.md" {
		t.Fatalf("排序丢失: %v", order)
	}
	if count, err := s.MigrateFilenames(); err != nil || count != 0 {
		t.Fatalf("迁移不是幂等的: %d %v", count, err)
	}
}

func TestMigrationRejectsCollisionsWithoutMoving(t *testing.T) {
	s, dir := testStore(t)
	put(t, dir, "a_20260101-abcdef.md", "a")
	put(t, dir, "a.md", "existing")
	put(t, dir, "b_20260101-ghijkl.md", "b")
	if _, err := s.MigrateFilenames(); !errors.Is(err, ErrConflict) {
		t.Fatalf("应拒绝覆盖: %v", err)
	}
	requireFile(t, dir, "a_20260101-abcdef.md", "a")
	requireFile(t, dir, "a.md", "existing")
	requireFile(t, dir, "b_20260101-ghijkl.md", "b")
}

func TestCatalogRejectsConcurrentOpenAndSymlink(t *testing.T) {
	s, dir := testStore(t)
	if other, err := Open(dir); err == nil {
		other.Close()
		t.Fatal("应拒绝第二个文档库实例")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	unsafeDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.sqlite")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(unsafeDir, DatabaseFile)); err != nil {
		t.Skip(err)
	}
	if other, err := Open(unsafeDir); err == nil {
		other.Close()
		t.Fatal("应拒绝数据库符号链接")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "unchanged" {
		t.Fatal("修改了根目录外的文件")
	}
}

func TestLegacyTrashMigrationAndRestore(t *testing.T) {
	dir := t.TempDir()
	id := "20260101T000000Z-abcdef"
	name := "old_20260101-abcdef.md"
	put(t, dir, payloadPath(id, name), "old content")
	put(t, dir, TrashPath(id)+"/meta.json", `{"deletedAt":"2026-01-01T00:00:00Z","items":[{"path":"old_20260101-abcdef.md","type":"file","size":11}]}`)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if count, err := s.MigrateFilenames(); err != nil || count != 1 {
		t.Fatalf("旧回收站迁移失败: %d %v", count, err)
	}
	entry, err := s.GetTrash(id)
	if err != nil || entry.Items[0].Path != "old.md" {
		t.Fatalf("旧回收站路径未更新: %+v %v", entry, err)
	}
	if err := s.Restore([]Trash{entry}); err != nil {
		t.Fatal(err)
	}
	doc, err := s.FindPath("old.md")
	if err != nil || doc.ID != "20260101-abcdef" {
		t.Fatalf("旧 ID 丢失: %+v %v", doc, err)
	}
	requireFile(t, dir, "old.md", "old content")
}

func TestDownloadEnabledDefaultsAndPersists(t *testing.T) {
	s, dir := testStore(t)
	if enabled, err := s.DownloadEnabled(); err != nil || enabled {
		t.Fatalf("默认下载开关 = %v, %v", enabled, err)
	}
	if err := s.SetDownloadEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled, err := reopened.DownloadEnabled(); err != nil || !enabled {
		t.Fatalf("重开后的下载开关 = %v, %v", enabled, err)
	}
	if err := reopened.SetDownloadEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if enabled, err := again.DownloadEnabled(); err != nil || enabled {
		t.Fatalf("关闭并重开后的下载开关 = %v, %v", enabled, err)
	}
}

func TestArticleMetaDefaultsAndPersists(t *testing.T) {
	s, dir := testStore(t)
	if meta, err := s.ArticleMeta(); err != nil || meta.ShowFileType || meta.ShowFileSize {
		t.Fatalf("默认文章信息设置 = %+v, %v", meta, err)
	}
	for _, meta := range []ArticleMeta{
		{ShowFileType: true, ShowFileSize: true},
		{ShowFileType: true, ShowFileSize: false},
		{ShowFileType: false, ShowFileSize: true},
		{ShowFileType: false, ShowFileSize: false},
	} {
		if err := s.SetArticleMeta(meta); err != nil {
			t.Fatal(err)
		}
		if got, err := s.ArticleMeta(); err != nil || got != meta {
			t.Fatalf("保存的文章信息设置 = %+v, %v，预期 %+v", got, err, meta)
		}
	}
	if err := s.SetArticleMeta(ArticleMeta{ShowFileType: true, ShowFileSize: false}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if meta, err := reopened.ArticleMeta(); err != nil || !meta.ShowFileType || meta.ShowFileSize {
		t.Fatalf("重开后的文章信息设置 = %+v, %v", meta, err)
	}
}

func TestInterruptedPurgeResumesAtStartup(t *testing.T) {
	s, dir := testStore(t)
	put(t, dir, "a.md", "a")
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	entry := Trash{ID: NewID(), DeletedAt: time.Now().UTC(), Items: []Item{{Path: "a.md", Type: "file"}}}
	if err := s.Delete(entry, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.pending(operation{Purge: []string{entry.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.TrashList()
	if err != nil || len(entries) != 0 {
		t.Fatalf("未恢复彻底删除: %+v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(dir, TrashPath(entry.ID))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("遗留已彻底删除的文件: %v", err)
	}
}
