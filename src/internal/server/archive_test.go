package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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
	if len(archive) != len(files)+1 {
		t.Fatalf("ZIP 文件 = %v", archive)
	}
	for _, name := range []string{"page/", "page/assets/", "page/page.md"} {
		if _, exists := archive[name]; !exists {
			t.Errorf("缺少 ZIP 条目: %s", name)
		}
	}
	for name, expected := range files {
		if name == "notes/deep/other.md" || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".css") {
			continue
		}
		archiveName := "page/assets/" + filepath.Base(name)
		if archive[archiveName] != expected {
			t.Errorf("素材 %s = %q, want %q", archiveName, archive[archiveName], expected)
		}
	}
	for _, expected := range []string{
		"![共享](assets/pic%20one.png?v=2#view)",
		"![本地](assets/local.png)",
		"![根目录](assets/logo.png)",
		"[picture]: <assets/%E4%B8%AD%E6%96%87%20%281%29.png> \"说明\"",
		"![转义](assets/a%281%29.png)",
		"![实体](assets/a&amp;b.png)",
		"[附件](assets/manual.pdf)",
		"[网页](assets/example.html)",
		"[文档](other.md)",
		`poster="assets/poster.jpg"`,
		`src="assets/clip.mp4"`,
		`srcset="assets/pic%20one.png 1x, assets/logo.png 2x"`,
		"![外链](https://example.com/a.png)",
		"```md\n![示例](assets/pic%20one.png)\n```",
	} {
		if !strings.Contains(archive["page/page.md"], expected) {
			t.Errorf("导出的 Markdown 缺少 %q\n%s", expected, archive["page/page.md"])
		}
	}
	for name, expected := range map[string]string{
		"page/assets/example.html": `href="theme.css"`,
		"page/assets/theme.css":    `url('logo.png')`,
		"page/assets/base.css":     `url('font.woff2')`,
	} {
		if !strings.Contains(archive[name], expected) {
			t.Errorf("%s 缺少 %q: %s", name, expected, archive[name])
		}
	}
}

func TestDocumentArchiveRenamesConflictingResources(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"notes/page.md":         "![共享](assets/logo.png)\n![本地](images/logo.png)\n![大小写](/shared/Logo.png)\n![编号](/other/logo-2.png)\n[网页](assets/demo.html)\n",
		"assets/logo.png":       "shared",
		"notes/images/logo.png": "local",
		"shared/Logo.png":       "case",
		"other/logo-2.png":      "numbered",
		"assets/demo.html":      `<link rel="stylesheet" href="site.css"><img src="/notes/images/logo.png">`,
		"assets/site.css":       `.x { background: url('/shared/Logo.png'); }`,
	}
	for name, data := range files {
		writeTestFile(t, root, name, []byte(data))
	}
	server := newTestServer(t, root, nil)
	id := documentIDFor(t, server, "notes/page.md")
	response := request(server, http.MethodGet, "/api/doc/archive?id="+id)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	archive := archiveFiles(t, response.Body.Bytes())
	if len(archive) != len(files)+2 {
		t.Fatalf("ZIP 文件 = %v", archive)
	}
	for name, expected := range map[string]string{
		"page/assets/logo.png":     "shared",
		"page/assets/logo-2.png":   "local",
		"page/assets/Logo-3.png":   "case",
		"page/assets/logo-2-2.png": "numbered",
	} {
		if archive[name] != expected {
			t.Errorf("素材 %s = %q, want %q", name, archive[name], expected)
		}
	}
	for name, parts := range map[string][]string{
		"page/page.md":          {"![共享](assets/logo.png)", "![本地](assets/logo-2.png)", "![大小写](assets/Logo-3.png)", "![编号](assets/logo-2-2.png)", "[网页](assets/demo.html)"},
		"page/assets/demo.html": {`href="site.css"`, `src="logo-2.png"`},
		"page/assets/site.css":  {`url('Logo-3.png')`},
	} {
		for _, part := range parts {
			if !strings.Contains(archive[name], part) {
				t.Errorf("%s 缺少 %q: %s", name, part, archive[name])
			}
		}
	}
}

func TestDocumentArchiveRenamesUnicodeEquivalentResources(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "page.md", []byte("![a](/one/%C3%A9.png)\n![b](/two/e%CC%81.png)\n![c](/three/%CE%A3.png)\n![d](/four/%CF%82.png)\n"))
	writeTestFile(t, root, "one/é.png", []byte("composed"))
	writeTestFile(t, root, "two/é.png", []byte("decomposed"))
	writeTestFile(t, root, "three/Σ.png", []byte("upper"))
	writeTestFile(t, root, "four/ς.png", []byte("lower"))
	server := newTestServer(t, root, nil)
	id := documentIDFor(t, server, "page.md")
	response := request(server, http.MethodGet, "/api/doc/archive?id="+id)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	archive := archiveFiles(t, response.Body.Bytes())
	for name, expected := range map[string]string{
		"page/assets/é.png":    "composed",
		"page/assets/é-2.png": "decomposed",
		"page/assets/ς.png":    "lower",
		"page/assets/Σ-2.png":  "upper",
	} {
		if archive[name] != expected {
			t.Errorf("等价文件名 %s = %q, want %q", name, archive[name], expected)
		}
	}
	for _, part := range []string{"assets/e%CC%81-2.png", "assets/%CE%A3-2.png"} {
		if !strings.Contains(archive["page/page.md"], part) {
			t.Errorf("重名素材引用缺少 %q: %s", part, archive["page/page.md"])
		}
	}
}

func TestDocumentArchiveEscapesSrcsetFilename(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "page.md", []byte(`<img srcset="/shared/logo%2C2.png 1x">`))
	writeTestFile(t, root, "shared/logo,2.png", []byte("image"))
	server := newTestServer(t, root, nil)
	id := documentIDFor(t, server, "page.md")
	response := request(server, http.MethodGet, "/api/doc/archive?id="+id)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	archive := archiveFiles(t, response.Body.Bytes())
	if archive["page/assets/logo,2.png"] != "image" {
		t.Fatalf("ZIP 素材 = %v", archive)
	}
	if !strings.Contains(archive["page/page.md"], `srcset="assets/logo%2C2.png 1x"`) {
		t.Fatalf("srcset 路径错误: %s", archive["page/page.md"])
	}
}

func TestDocumentArchiveWithoutResources(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name   string
		source string
	}{
		{name: "notes/empty.md", source: ""},
		{name: "notes/page.md", source: "# 文档\n![外部](https://example.com/p.png)\n[其他文档](other.md)\n"},
		{name: "notes/self.md", source: "# 自引用\n[自身](/notes/self.md)\n"},
	}
	for _, test := range tests {
		writeTestFile(t, root, test.name, []byte(test.source))
	}
	writeTestFile(t, root, "notes/other.md", []byte("另一个文档"))
	server := newTestServer(t, root, nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := documentIDFor(t, server, test.name)
			response := request(server, http.MethodGet, "/api/doc/archive?id="+id)
			if response.Code != http.StatusOK {
				t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
			}
			reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if len(reader.File) != 1 || reader.File[0].Name != filepath.Base(test.name) || reader.File[0].FileInfo().IsDir() {
				t.Fatalf("ZIP 条目 = %v", reader.File)
			}
			file, err := reader.File[0].Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil || string(data) != test.source {
				t.Fatalf("文档内容 = %q, err = %v", data, err)
			}
		})
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

func TestDocumentAssetFlagMatchesArchive(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"notes/empty.md":        "",
		"notes/external.md":     "![外链](https://example.com/a.png)\n[文档](other.md)",
		"notes/missing.md":      "![缺失](assets/missing.png)",
		"notes/shared.md":       "![图片](assets/pic.png)",
		"notes/local.md":        "[附件](docs/report.pdf)",
		"notes/code.md":         "```md\n![示例](assets/pic.png)\n```",
		"notes/symlink.md":      "![链接](assets/link.png)",
		"notes/other.md":        "# 其他文档",
		"assets/pic.png":        "image",
		"notes/docs/report.pdf": "pdf",
	}
	for name, data := range files {
		writeTestFile(t, root, name, []byte(data))
	}
	outside := t.TempDir()
	writeTestFile(t, outside, "image.png", []byte("outside"))
	if err := os.Symlink(filepath.Join(outside, "image.png"), filepath.Join(root, "assets", "link.png")); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, root, nil)
	for _, test := range []struct {
		name string
		want bool
	}{
		{"notes/empty.md", false},
		{"notes/external.md", false},
		{"notes/missing.md", false},
		{"notes/shared.md", true},
		{"notes/local.md", true},
		{"notes/code.md", false},
		{"notes/symlink.md", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := documentIDFor(t, server, test.name)
			response := request(server, http.MethodGet, "/api/doc?id="+id)
			if response.Code != http.StatusOK {
				t.Fatalf("文档接口状态码 = %d", response.Code)
			}
			var payload struct {
				Data struct {
					HasAssets bool `json:"hasAssets"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			archive := request(server, http.MethodGet, "/api/doc/archive?id="+id)
			if archive.Code != http.StatusOK {
				t.Fatalf("压缩包接口状态码 = %d", archive.Code)
			}
			containsAssets := len(archiveFiles(t, archive.Body.Bytes())) > 1
			if payload.Data.HasAssets != test.want || payload.Data.HasAssets != containsAssets {
				t.Fatalf("素材标记 = %v，ZIP 含素材 = %v，预期 = %v", payload.Data.HasAssets, containsAssets, test.want)
			}
		})
	}
}
