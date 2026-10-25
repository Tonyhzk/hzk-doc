package server

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"testing/fstest"
)

func TestSiteAndFrontendServing(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<html>app</html>")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body{}")},
	}
	docsRoot := t.TempDir()
	writeTestFile(t, docsRoot, "a.md", []byte("a"))
	server, err := newServer(Config{
		DocsDir:      docsRoot,
		SiteName:     "测试站",
		Version:      "1.2.3",
		AdminEnabled: true,
	}, assets, newOptions{logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatalf("创建服务失败: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	siteRecorder := request(server, http.MethodGet, "/api/site")
	if siteRecorder.Code != http.StatusOK {
		t.Fatalf("站点接口状态码 = %d", siteRecorder.Code)
	}
	site := decodeJSON[struct {
		Success bool       `json:"success"`
		Data    siteConfig `json:"data"`
	}](t, siteRecorder)
	if !site.Success || site.Data.Name != "测试站" || site.Data.Version != "1.2.3" || !site.Data.AdminEnabled || site.Data.Count != 1 {
		t.Fatalf("站点信息 = %+v", site.Data)
	}

	index := request(server, http.MethodGet, "/")
	if index.Code != http.StatusOK || index.Body.String() != "<html>app</html>" {
		t.Fatalf("首页 = %d %q", index.Code, index.Body.String())
	}
	spa := request(server, http.MethodGet, "/admin")
	if spa.Code != http.StatusOK || spa.Body.String() != "<html>app</html>" {
		t.Fatalf("后台回退 = %d %q", spa.Code, spa.Body.String())
	}
	if unknown := request(server, http.MethodGet, "/doc/foo"); unknown.Code != http.StatusNotFound {
		t.Fatalf("未知页面状态码 = %d，预期 404", unknown.Code)
	}
	for _, target := range []string{"/api/config", "/asset?path=assets/app.css", "/foo"} {
		recorder := request(server, http.MethodGet, target)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s 状态码 = %d，预期 404", target, recorder.Code)
		}
	}
	css := request(server, http.MethodGet, "/assets/app.css")
	if css.Code != http.StatusOK || css.Body.String() != "body{}" {
		t.Fatalf("CSS = %d %q", css.Code, css.Body.String())
	}
	if css.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("CSS 内容类型 = %q", css.Header().Get("Content-Type"))
	}
	if missing := request(server, http.MethodGet, "/missing.js"); missing.Code != http.StatusNotFound {
		t.Fatalf("缺失静态文件状态码 = %d", missing.Code)
	}
	unknownAPI := request(server, http.MethodGet, "/api/unknown")
	if unknownAPI.Code != http.StatusNotFound || unknownAPI.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("未知接口 = %d %q", unknownAPI.Code, unknownAPI.Header().Get("Content-Type"))
	}
}
