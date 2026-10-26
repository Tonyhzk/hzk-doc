package integration

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"

	"hzk-doc/internal/admin"
	"hzk-doc/internal/catalog"
	"hzk-doc/internal/server"
)

const lifecyclePassword = "integration-test-password"

type lifecycleApp struct {
	t       *testing.T
	docs    string
	store   *catalog.Store
	reader  *server.Server
	manager *admin.Handler
	server  *httptest.Server
	client  *http.Client
	address string
	csrf    string
	closed  bool
}

type lifecycleDocument struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Revision   string `json:"revision"`
	RawContent string `json:"rawContent"`
}

type lifecycleSource struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

type lifecycleTreeEntry struct {
	Type     string               `json:"type"`
	Path     string               `json:"path"`
	ID       string               `json:"id"`
	Children []lifecycleTreeEntry `json:"children"`
}

type lifecycleTrashEntry struct {
	ID    string `json:"id"`
	Items []struct {
		Path string `json:"path"`
	} `json:"items"`
}

func newLifecycleApp(t *testing.T, docs string) *lifecycleApp {
	t.Helper()

	store, err := catalog.Open(docs)
	if err != nil {
		t.Fatalf("打开文档目录失败: %v", err)
	}
	reader, err := server.New(server.Config{
		Catalog: store, DocsDir: docs, SiteName: "测试站", Version: "test", AdminEnabled: true,
	}, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html lang=\"zh-CN\"></html>")}})
	if err != nil {
		_ = store.Close()
		t.Fatalf("创建阅读端失败: %v", err)
	}
	manager, err := admin.New(admin.Config{
		Catalog: store, DocsDir: docs, Password: lifecyclePassword,
		OnChange: reader.Refresh, Preview: reader.PreviewMarkdown,
	})
	if err != nil {
		_ = reader.Close()
		_ = store.Close()
		t.Fatalf("创建后台失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/admin/", manager)
	mux.Handle("/", reader)
	app := httptest.NewServer(mux)

	jar, err := cookiejar.New(nil)
	if err != nil {
		app.Close()
		_ = manager.Close()
		_ = reader.Close()
		_ = store.Close()
		t.Fatalf("创建 Cookie 容器失败: %v", err)
	}
	client := &http.Client{Jar: jar}

	login := request(t, client, "POST", app.URL+"/api/admin/login", map[string]string{"password": lifecyclePassword}, "", 200)
	var session struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login, &session); err != nil || !session.Authenticated || session.CSRFToken == "" {
		app.Close()
		_ = manager.Close()
		_ = reader.Close()
		_ = store.Close()
		t.Fatalf("登录会话不可用: %s, %v", login, err)
	}

	instance := &lifecycleApp{
		t: t, docs: docs, store: store, reader: reader, manager: manager,
		server: app, client: client, address: app.URL, csrf: session.CSRFToken,
	}
	t.Cleanup(instance.close)
	return instance
}

func (app *lifecycleApp) close() {
	if app.closed {
		return
	}
	app.closed = true
	app.server.Close()
	if err := app.manager.Close(); err != nil {
		app.t.Errorf("关闭后台失败: %v", err)
	}
	if err := app.reader.Close(); err != nil {
		app.t.Errorf("关闭阅读端失败: %v", err)
	}
	if err := app.store.Close(); err != nil {
		app.t.Errorf("关闭文档目录失败: %v", err)
	}
}

func (app *lifecycleApp) do(method, target string, payload any, wantStatus int) json.RawMessage {
	app.t.Helper()
	return request(app.t, app.client, method, app.address+target, payload, app.csrf, wantStatus)
}

func (app *lifecycleApp) createDocument(name, content string) lifecycleDocument {
	app.t.Helper()
	var document lifecycleDocument
	data := app.do("POST", "/api/admin/document", map[string]string{"path": name, "content": content}, 201)
	if err := json.Unmarshal(data, &document); err != nil {
		app.t.Fatalf("解析新建结果失败: %s, %v", data, err)
	}
	if document.ID == "" || document.Path != name {
		app.t.Fatalf("新建文档 = %+v", document)
	}
	return document
}

func (app *lifecycleApp) document(id string) lifecycleDocument {
	app.t.Helper()
	var document lifecycleDocument
	data := app.do("GET", "/api/doc?id="+url.QueryEscape(id), nil, 200)
	if err := json.Unmarshal(data, &document); err != nil {
		app.t.Fatalf("解析文档失败: %s, %v", data, err)
	}
	if document.ID != id {
		app.t.Fatalf("文档 ID = %q，预期 %q", document.ID, id)
	}
	return document
}

func (app *lifecycleApp) deleteDocument(name string) lifecycleTrashEntry {
	app.t.Helper()
	var entry lifecycleTrashEntry
	data := app.do("POST", "/api/admin/delete", map[string]string{"path": name}, 200)
	if err := json.Unmarshal(data, &entry); err != nil {
		app.t.Fatalf("解析删除结果失败: %s, %v", data, err)
	}
	if entry.ID == "" {
		app.t.Fatalf("删除结果缺少回收站条目: %s", data)
	}
	return entry
}

func (app *lifecycleApp) treeIDs() map[string]string {
	app.t.Helper()
	data := app.do("GET", "/api/tree", nil, 200)
	var entries []lifecycleTreeEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		app.t.Fatalf("解析目录失败: %s, %v", data, err)
	}

	ids := make(map[string]string)
	var walk func([]lifecycleTreeEntry)
	walk = func(nodes []lifecycleTreeEntry) {
		for _, node := range nodes {
			if node.Type == "folder" {
				walk(node.Children)
				continue
			}
			ids[node.Path] = node.ID
		}
	}
	walk(entries)
	return ids
}

func TestDocumentIDLifecycle(t *testing.T) {
	app := newLifecycleApp(t, t.TempDir())

	created := app.createDocument("指南.md", "# 指南\n\n初始内容")
	if got := app.treeIDs()["指南.md"]; got != created.ID {
		t.Fatalf("目录中的 ID = %q，预期 %q", got, created.ID)
	}

	var renamed lifecycleDocument
	data := app.do("POST", "/api/admin/rename", map[string]string{"path": "指南.md", "name": "手册.md"}, 200)
	if err := json.Unmarshal(data, &renamed); err != nil {
		t.Fatalf("解析改名结果失败: %s, %v", data, err)
	}
	if renamed.ID != created.ID || renamed.Path != "手册.md" {
		t.Fatalf("改名结果 = %+v，预期 ID %q 路径 手册.md", renamed, created.ID)
	}
	if document := app.document(created.ID); document.Path != "手册.md" {
		t.Fatalf("改名后按 ID 读取 = %+v", document)
	}
	ids := app.treeIDs()
	if ids["手册.md"] != created.ID {
		t.Fatalf("改名后目录 ID = %q，预期 %q", ids["手册.md"], created.ID)
	}
	if _, ok := ids["指南.md"]; ok {
		t.Fatal("改名后仍存在旧路径")
	}

	var source lifecycleSource
	data = app.do("GET", "/api/admin/source?path="+url.QueryEscape("手册.md"), nil, 200)
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatalf("解析源文件失败: %s, %v", data, err)
	}
	if source.ID != created.ID || source.Revision == "" {
		t.Fatalf("源文件 = %+v，预期 ID %q", source, created.ID)
	}
	var edited lifecycleDocument
	data = app.do("PUT", "/api/admin/document", map[string]string{
		"path": "手册.md", "content": "# 手册\n\n编辑后内容", "revision": source.Revision,
	}, 200)
	if err := json.Unmarshal(data, &edited); err != nil {
		t.Fatalf("解析编辑结果失败: %s, %v", data, err)
	}
	if edited.ID != created.ID {
		t.Fatalf("编辑后 ID = %q，预期 %q", edited.ID, created.ID)
	}
	if document := app.document(created.ID); document.RawContent != "# 手册\n\n编辑后内容" {
		t.Fatalf("编辑后按 ID 读取 = %+v", document)
	}

	oldEntry := app.deleteDocument("手册.md")
	if len(oldEntry.Items) != 1 || oldEntry.Items[0].Path != "手册.md" {
		t.Fatalf("删除条目 = %+v", oldEntry)
	}
	app.do("GET", "/api/doc?id="+url.QueryEscape(created.ID), nil, 404)
	if _, ok := app.treeIDs()["手册.md"]; ok {
		t.Fatal("删除后目录仍列出文档")
	}

	recreated := app.createDocument("手册.md", "# 新手册\n\n新内容")
	if recreated.ID == created.ID {
		t.Fatalf("同名重建复用了旧 ID %q", created.ID)
	}
	if document := app.document(recreated.ID); document.RawContent != "# 新手册\n\n新内容" {
		t.Fatalf("同名重建后读取 = %+v", document)
	}

	app.do("POST", "/api/admin/trash/restore", map[string]any{"ids": []string{oldEntry.ID}}, 409)
	if document := app.document(recreated.ID); document.RawContent != "# 新手册\n\n新内容" {
		t.Fatalf("还原冲突后新文档被覆盖: %+v", document)
	}
	app.do("GET", "/api/doc?id="+url.QueryEscape(created.ID), nil, 404)

	newEntry := app.deleteDocument("手册.md")
	app.do("POST", "/api/admin/trash/restore", map[string]any{"ids": []string{oldEntry.ID}}, 200)
	restored := app.document(created.ID)
	if restored.Path != "手册.md" || restored.RawContent != "# 手册\n\n编辑后内容" {
		t.Fatalf("还原后文档 = %+v", restored)
	}
	if got := app.treeIDs()["手册.md"]; got != created.ID {
		t.Fatalf("还原后目录 ID = %q，预期 %q", got, created.ID)
	}

	app.do("POST", "/api/admin/trash/purge", map[string]any{"ids": []string{newEntry.ID}}, 200)
	app.do("GET", "/api/doc?id="+url.QueryEscape(recreated.ID), nil, 404)

	app.close()
	app = newLifecycleApp(t, app.docs)
	if document := app.document(created.ID); document.Path != "手册.md" || document.RawContent != "# 手册\n\n编辑后内容" {
		t.Fatalf("重启后按 ID 读取 = %+v", document)
	}
	if got := app.treeIDs()["手册.md"]; got != created.ID {
		t.Fatalf("重启后目录 ID = %q，预期 %q", got, created.ID)
	}
	app.do("GET", "/api/doc?id="+url.QueryEscape(recreated.ID), nil, 404)
}

func TestRenameConflictKeepsDocuments(t *testing.T) {
	app := newLifecycleApp(t, t.TempDir())

	first := app.createDocument("指南.md", "# 指南\n\n原内容")
	second := app.createDocument("手册.md", "# 手册\n\n目标内容")

	app.do("POST", "/api/admin/rename", map[string]string{"path": "指南.md", "name": "手册.md"}, 409)

	for _, want := range []struct {
		id      string
		path    string
		content string
	}{
		{first.ID, "指南.md", "# 指南\n\n原内容"},
		{second.ID, "手册.md", "# 手册\n\n目标内容"},
	} {
		document := app.document(want.id)
		if document.Path != want.path || document.RawContent != want.content {
			t.Fatalf("改名冲突后文档 = %+v，预期路径 %q 内容 %q", document, want.path, want.content)
		}
	}
	ids := app.treeIDs()
	if ids["指南.md"] != first.ID || ids["手册.md"] != second.ID {
		t.Fatalf("改名冲突后目录 = %v，预期 %q/%q", ids, first.ID, second.ID)
	}
}
