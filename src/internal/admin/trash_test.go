package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hzk-doc/internal/content"
)

func (e *testEnv) deletePath(name string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := jsonBody(e.t, deleteRequest{Path: name})
	return e.authRequest(http.MethodPost, "/api/admin/delete", body)
}

func (e *testEnv) trashList() trashData {
	e.t.Helper()
	recorder := e.authRequest(http.MethodGet, "/api/admin/trash", nil)
	return decodeData[trashData](e.t, recorder)
}

func (e *testEnv) trashAction(action string, ids ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := jsonBody(e.t, trashIDsRequest{IDs: ids})
	return e.authRequest(http.MethodPost, "/api/admin/trash/"+action, body)
}

func (e *testEnv) emptyTrash() *httptest.ResponseRecorder {
	e.t.Helper()
	return e.authRequest(http.MethodPost, "/api/admin/trash/empty", []byte(`{}`))
}

func exists(docs, name string) bool {
	_, err := os.Lstat(filepath.Join(docs, filepath.FromSlash(name)))
	return err == nil
}

func TestDeleteMovesDocumentAndOrphanAsset(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "guide.md", "![图](assets/only.png)")
	writeDocFile(t, env.docs, "assets/only.png", "png")
	writeDocFile(t, env.docs, "assets/shared.png", "png")
	writeDocFile(t, env.docs, "other.md", "![图](assets/shared.png)")

	recorder := env.deletePath("guide.md")
	entry := decodeData[trashEntry](t, recorder)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(entry.Items) != 2 {
		t.Fatalf("items = %+v, want document and orphan asset", entry.Items)
	}
	paths := make([]string, 0, len(entry.Items))
	for _, item := range entry.Items {
		paths = append(paths, item.Path)
	}
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "guide.md") || !strings.Contains(joined, "assets/only.png") {
		t.Fatalf("items = %v", paths)
	}

	if exists(env.docs, "guide.md") || exists(env.docs, "assets/only.png") {
		t.Fatal("删除后原位置仍存在文件")
	}
	if !exists(env.docs, "assets/shared.png") {
		t.Fatal("被其他文档引用的资源不应移动")
	}
	if !exists(env.docs, content.TrashDir) {
		t.Fatal("回收站目录未创建")
	}

	data := env.trashList()
	if len(data.Entries) != 1 || data.Entries[0].ID != entry.ID {
		t.Fatalf("trash entries = %+v", data.Entries)
	}
	if data.Size <= 0 {
		t.Fatalf("trash size = %d", data.Size)
	}
}

func TestDeleteKeepsSharedAssets(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "a.md", "![图](assets/pic.png)")
	writeDocFile(t, env.docs, "b.md", "![图](assets/pic.png)")
	writeDocFile(t, env.docs, "assets/pic.png", "png")

	entry := decodeData[trashEntry](t, env.deletePath("a.md"))
	if len(entry.Items) != 1 || entry.Items[0].Path != "a.md" {
		t.Fatalf("items = %+v, want only document", entry.Items)
	}
	if !exists(env.docs, "assets/pic.png") {
		t.Fatal("共用资源被错误移动")
	}
}

func TestDeleteFolderMovesOrphanAssets(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "notes/a.md", "![图](../assets/only.png)")
	writeDocFile(t, env.docs, "assets/only.png", "png")

	entry := decodeData[trashEntry](t, env.deletePath("notes"))
	if len(entry.Items) != 2 {
		t.Fatalf("items = %+v, want folder and orphan asset", entry.Items)
	}
	if exists(env.docs, "notes") || exists(env.docs, "assets/only.png") {
		t.Fatal("删除目录后仍存在内容")
	}

	restored := env.trashAction("restore", entry.ID)
	expectStatus(t, restored, http.StatusOK)
	if !exists(env.docs, "notes/a.md") || !exists(env.docs, "assets/only.png") {
		t.Fatal("还原后文件缺失")
	}
}

func TestRestoreMovesFilesBack(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "deep/doc.md", "内容")
	writeDocFile(t, env.docs, "asset.png", "png")
	writeDocFile(t, env.docs, "page.html", `<img src="asset.png">`)

	entry := decodeData[trashEntry](t, env.deletePath("deep/doc.md"))
	expectStatus(t, env.trashAction("restore", entry.ID), http.StatusOK)

	if !exists(env.docs, "deep/doc.md") {
		t.Fatal("文档未还原")
	}
	if got := readDocFile(t, env.docs, "deep/doc.md"); got != "内容" {
		t.Fatalf("还原内容 = %q", got)
	}
	if len(env.trashList().Entries) != 0 {
		t.Fatal("还原后回收站应清空该条目")
	}

	pageEntry := decodeData[trashEntry](t, env.deletePath("page.html"))
	if len(pageEntry.Items) != 2 {
		t.Fatalf("HTML 资源未随文档回收: %+v", pageEntry.Items)
	}
}

func TestRestoreRejectsExistingTarget(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "doc.md", "原始")

	entry := decodeData[trashEntry](t, env.deletePath("doc.md"))
	writeDocFile(t, env.docs, "doc.md", "新内容")

	expectStatus(t, env.trashAction("restore", entry.ID), http.StatusConflict)
	if got := readDocFile(t, env.docs, "doc.md"); got != "新内容" {
		t.Fatalf("冲突时不应覆盖现有文件: %q", got)
	}
	if len(env.trashList().Entries) != 1 {
		t.Fatal("冲突后回收站条目应保留")
	}
}

func TestPurgeAndEmptyTrash(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "a.md", "a")
	writeDocFile(t, env.docs, "b.md", "b")

	first := decodeData[trashEntry](t, env.deletePath("a.md"))
	second := decodeData[trashEntry](t, env.deletePath("b.md"))

	expectStatus(t, env.trashAction("purge", first.ID), http.StatusOK)
	if len(env.trashList().Entries) != 1 {
		t.Fatal("单独删除条目失败")
	}
	if exists(env.docs, content.TrashDir+"/entries/"+first.ID) {
		t.Fatal("回收站条目不应当被保留")
	}

	result := decodeData[trashActionResult](t, env.emptyTrash())
	if result.Count != 1 {
		t.Fatalf("清空数量 = %d, want 1", result.Count)
	}
	if len(env.trashList().Entries) != 0 {
		t.Fatal("清空回收站失败")
	}
	_ = second
}

func TestTrashValidation(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	expectStatus(t, env.deletePath(""), http.StatusBadRequest)
	expectStatus(t, env.deletePath(".hzk-doc-order.json"), http.StatusBadRequest)
	expectStatus(t, env.deletePath("../escape.md"), http.StatusBadRequest)
	expectStatus(t, env.deletePath("missing.md"), http.StatusNotFound)
	expectStatus(t, env.trashAction("restore", "not-an-id"), http.StatusBadRequest)
	expectStatus(t, env.trashAction("purge"), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/delete", []byte(`{}`)), http.StatusBadRequest)
}

func TestDeleteRequiresAuthAndCsrf(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, deleteRequest{Path: "doc.md"})

	expectStatus(t, env.request(http.MethodPost, "/api/admin/delete", body), http.StatusUnauthorized)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/trash", nil), http.StatusUnauthorized)

	env.mustLogin()
	writeDocFile(t, env.docs, "doc.md", "内容")
	expectStatus(t, env.request(http.MethodPost, "/api/admin/delete", body, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: env.token})
	}), http.StatusForbidden)
	if !exists(env.docs, "doc.md") {
		t.Fatal("缺少 CSRF 时不应删除文件")
	}
}
