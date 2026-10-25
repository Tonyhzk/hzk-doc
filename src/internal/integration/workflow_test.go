package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"hzk-doc/internal/admin"
	"hzk-doc/internal/server"
)

func TestManagementUpdatesReader(t *testing.T) {
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "README.md"), []byte("# 首页"), 0644); err != nil {
		t.Fatal(err)
	}
	reader, err := server.New(server.Config{
		DocsDir: docs, SiteName: "测试站", Version: "test", AdminEnabled: true,
	}, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html lang=\"zh-CN\"></html>")}})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	manager, err := admin.New(admin.Config{
		DocsDir: docs, Password: "integration-test-password",
		OnChange: reader.Refresh, Preview: reader.PreviewMarkdown,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	mux := http.NewServeMux()
	mux.Handle("/api/admin/", manager)
	mux.Handle("/", reader)
	app := httptest.NewServer(mux)
	defer app.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login := request(t, client, "POST", app.URL+"/api/admin/login", map[string]string{"password": "integration-test-password"}, "", 200)
	var session struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login, &session); err != nil || !session.Authenticated || session.CSRFToken == "" {
		t.Fatalf("登录会话不可用: %s, %v", login, err)
	}
	request(t, client, "POST", app.URL+"/api/admin/document", map[string]string{"path": "forbidden.md", "content": "拒绝写入"}, "", 403)
	if _, err := os.Stat(filepath.Join(docs, "forbidden.md")); !os.IsNotExist(err) {
		t.Fatalf("缺少CSRF的请求产生了文件: %v", err)
	}
	name := "新文档.md"
	request(t, client, "POST", app.URL+"/api/admin/document", map[string]string{"path": name, "content": "# 新文档\n\n集成检索关键字"}, session.CSRFToken, 201)
	search := request(t, client, "GET", app.URL+"/api/search?scope=full&q="+url.QueryEscape("集成检索"), nil, "", 200)
	var matches []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(search, &matches); err != nil || len(matches) != 1 || matches[0].Path != name {
		t.Fatalf("后台新建后搜索未更新: %s, %v", search, err)
	}

	source := request(t, client, "GET", app.URL+"/api/admin/source?path="+url.QueryEscape(name), nil, "", 200)
	var original struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(source, &original); err != nil || original.Revision == "" {
		t.Fatalf("未返回编辑版本: %s, %v", source, err)
	}
	request(t, client, "PUT", app.URL+"/api/admin/document", map[string]string{
		"path": name, "content": "# 更新\n\n新的搜索关键字", "revision": original.Revision,
	}, session.CSRFToken, 200)
	request(t, client, "PUT", app.URL+"/api/admin/document", map[string]string{
		"path": name, "content": "不应覆盖", "revision": original.Revision,
	}, session.CSRFToken, 409)
	search = request(t, client, "GET", app.URL+"/api/search?scope=full&q="+url.QueryEscape("新的搜索"), nil, "", 200)
	if err := json.Unmarshal(search, &matches); err != nil || len(matches) != 1 || matches[0].Path != name {
		t.Fatalf("后台编辑后搜索未更新: %s, %v", search, err)
	}
	request(t, client, "PUT", app.URL+"/api/admin/order", map[string]any{
		"directory": "", "names": []string{name, "README.md"},
	}, session.CSRFToken, 200)
	tree := request(t, client, "GET", app.URL+"/api/tree", nil, "", 200)
	var nodes []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(tree, &nodes); err != nil || len(nodes) != 2 || nodes[0].Path != name {
		t.Fatalf("公开目录没有使用后台排序: %s, %v", tree, err)
	}
	request(t, client, "POST", app.URL+"/api/admin/logout", nil, session.CSRFToken, 200)
	request(t, client, "GET", app.URL+"/api/admin/source?path="+url.QueryEscape(name), nil, "", 401)
}

func request(t *testing.T, client *http.Client, method, address string, payload any, token string, wantStatus int) json.RawMessage {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, address, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", req.URL.Scheme+"://"+req.URL.Host)
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s: 状态=%d 预期=%d 响应=%s", method, req.URL.Path, response.StatusCode, wantStatus, data)
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("响应不是JSON: %v", err)
	}
	if envelope.Success != (wantStatus >= 200 && wantStatus < 300) {
		t.Fatalf("响应协议与HTTP状态不一致: %s", data)
	}
	return envelope.Data
}
