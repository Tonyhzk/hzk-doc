package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newTestServer(t *testing.T, docsRoot string, configure func(*Config)) *Server {
	t.Helper()
	return newTestServerWithOptions(t, docsRoot, configure, nil)
}

func newTestServerWithOptions(t *testing.T, docsRoot string, configure func(*Config), configureOptions func(*newOptions)) *Server {
	t.Helper()
	config := Config{
		DocsDir:  docsRoot,
		SiteName: "测试文档",
		Version:  "test",
	}
	options := newOptions{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if configure != nil {
		configure(&config)
	}
	if configureOptions != nil {
		configureOptions(&options)
	}
	server, err := newServer(config, nil, options)
	if err != nil {
		t.Fatalf("创建服务失败: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("关闭服务失败: %v", err)
		}
	})
	return server
}

func request(server *Server, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(method, target, nil)
	server.ServeHTTP(recorder, httpRequest)
	return recorder
}

func writeTestFile(t *testing.T, root, name string, data []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("创建目录 %s 失败: %v", target, err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("写入文件 %s 失败: %v", target, err)
	}
}

func decodeJSON[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("解析 JSON %q 失败: %v", recorder.Body.String(), err)
	}
	return value
}

func findTreeFile(entries []TreeEntry, path string) (TreeEntry, bool) {
	for _, entry := range entries {
		if entry.Type == "file" && entry.Path == path {
			return entry, true
		}
		if entry.Type == "folder" {
			if found, ok := findTreeFile(entry.Children, path); ok {
				return found, true
			}
		}
	}
	return TreeEntry{}, false
}
