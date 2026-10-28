package server

import (
	"archive/zip"
	"bytes"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveFiles(t *testing.T, body []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("读取 ZIP 失败: %v", err)
	}
	files := make(map[string]string)
	for _, file := range reader.File {
		if _, exists := files[file.Name]; exists {
			t.Fatalf("重复的 ZIP 条目: %s", file.Name)
		}
		item, err := file.Open()
		if err != nil {
			t.Fatalf("打开 ZIP 条目失败: %v", err)
		}
		data, err := io.ReadAll(item)
		_ = item.Close()
		if err != nil {
			t.Fatalf("读取 ZIP 条目失败: %v", err)
		}
		files[file.Name] = string(data)
	}
	return files
}

func TestDocumentArchiveIncludesResourcesAndRelativePaths(t *testing.T) {
	root := t.TempDir()
	source := "# 测试\n\n" +
		"![共享](assets/pic%20one.png?v=2#view)\n" +
		"![本地](images/local.png)\n" +
		"![根目录](/shared/logo.png)\n" +
		"![参考][picture]\n" +
		"![转义](assets/a\\(1\\).png)\n" +
		"![实体](assets/a&amp;b.png)\n\n" +
		"[picture]: <assets/中文 (1).png> \"说明\"\n\n" +
		"[附件](assets/manual.pdf)\n[网页](assets/example.html)\n[文档](other.md)\n" +
		"<video poster=\"assets/poster.jpg\"><source src=\"assets/clip.mp4\"></video>\n" +
		"<img srcset=\"assets/pic%20one.png 1x, /shared/logo.png 2x\">\n" +
		"![外链](https://example.com/a.png)\n\n" +
		"```md\n![示例](assets/pic%20one.png)\n```\n"
	files := map[string]string{
		"notes/deep/page.md":          source,
		"notes/deep/other.md":         "另一个文档",
		"notes/deep/images/local.png": "local",
		"assets/pic one.png":          "picture",
		"assets/中文 (1).png":           "chinese",
		"assets/a(1).png":             "escaped",
		"assets/a&b.png":              "entity",
		"assets/manual.pdf":           "pdf",
		"assets/poster.jpg":           "poster",
		"assets/clip.mp4":             "video",
		"shared/logo.png":             "logo",
		"assets/example.html":         `<link rel="stylesheet" href="assets/theme.css"><img src="/shared/logo.png">`,
		"assets/theme.css":            `@import "theme/base.css"; .x{background:url('/shared/logo.png')}`,
		"assets/theme/base.css":       `@import "../theme.css"; @font-face{src:url('/shared/font.woff2')}`,
		"shared/font.woff2":           "font",
	}
	for name, data := range files {
		writeTestFile(t, root, name, []byte(data))
	}
	server := newTestServer(t, root, nil)
	id := documentIDFor(t, server, "notes/deep/page.md")
	response := request(server, http.MethodGet, "/api/doc/archive?id="+id)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("错误的 Content-Type: %s", response.Header().Get("Content-Type"))
	}
	_, disposition, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	if err != nil || disposition["filename"] != "page.zip" {
		t.Fatalf("错误的下载文件名: %v %v", disposition, err)
	}
	archive := archiveFiles(t, response.Body.Bytes())
	if len(archive) != len(files)-1 {
		t.Fatalf("ZIP 文件 = %v", archive)
	}
	for name, expected := range files {
		if name == "notes/deep/other.md" || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".css") {
			continue
		}
		if archive[name] != expected {
			t.Errorf("素材 %s = %q, want %q", name, archive[name], expected)
		}
	}
	for _, expected := range []string{
		"![共享](../../assets/pic%20one.png?v=2#view)",
		"![本地](images/local.png)",
		"![根目录](../../shared/logo.png)",
		"[picture]: <../../assets/%E4%B8%AD%E6%96%87%20%281%29.png> \"说明\"",
		"![转义](../../assets/a%281%29.png)",
		"![实体](../../assets/a&b.png)",
		"[附件](../../assets/manual.pdf)",
		"[网页](../../assets/example.html)",
		"[文档](other.md)",
		`poster="../../assets/poster.jpg"`,
		`src="../../assets/clip.mp4"`,
		`srcset="../../assets/pic%20one.png 1x, ../../shared/logo.png 2x"`,
		"![外链](https://example.com/a.png)",
		"```md\n![示例](assets/pic%20one.png)\n```",
	} {
		if !strings.Contains(archive["notes/deep/page.md"], expected) {
			t.Errorf("导出的 Markdown 缺少 %q\n%s", expected, archive["notes/deep/page.md"])
		}
	}
	for name, expected := range map[string]string{
		"assets/example.html":   `href="theme.css"`,
		"assets/theme.css":      `url('../shared/logo.png')`,
		"assets/theme/base.css": `url('../../shared/font.woff2')`,
	} {
		if !strings.Contains(archive[name], expected) {
			t.Errorf("%s 缺少 %q: %s", name, expected, archive[name])
		}
	}
}

func TestDocumentArchiveRejectsUnsafeAndMissingPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, outside, "secret.png", []byte("secret"))
	writeTestFile(t, root, "page.md", []byte("![a](assets/link.png)\n![b](../secret.png)\n![c](.hidden.png)\n![d](assets/missing.png)\n![e](assets/a%5Cb.png)\n"))
	writeTestFile(t, root, "other.pdf", []byte("pdf"))
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(root, "assets", "link.png")); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, root, nil)
	id := documentIDFor(t, server, "page.md")
	response := request(server, http.MethodGet, "/api/doc/archive?id="+id)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	archive := archiveFiles(t, response.Body.Bytes())
	if len(archive) != 1 || archive["page.md"] == "" {
		t.Fatalf("ZIP 包含不安全或不存在的素材: %v", archive)
	}
	for target, want := range map[string]int{
		"/api/doc/archive":            http.StatusBadRequest,
		"/api/doc/archive?id=missing": http.StatusNotFound,
		"/api/doc/archive?id=" + documentIDFor(t, server, "other.pdf"): http.StatusUnsupportedMediaType,
	} {
		if got := request(server, http.MethodGet, target).Code; got != want {
			t.Errorf("%s: 状态码 = %d, want %d", target, got, want)
		}
	}
	head := request(server, http.MethodHead, "/api/doc/archive?id="+id)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD 状态码 = %d, body = %q", head.Code, head.Body.String())
	}
}
