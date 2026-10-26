package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTreeSkipsHiddenAssetsAndSorts(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "guides/10-intro_20260101-abcdef.md", []byte("# intro"))
	writeTestFile(t, root, "guides/2-basics.md", []byte("# basics"))
	writeTestFile(t, root, "guides/demo.html", []byte("<html></html>"))
	writeTestFile(t, root, "files/a.pdf", []byte("%PDF-1.4"))
	writeTestFile(t, root, "images/pic.png", []byte("png"))
	writeTestFile(t, root, "office/a.docx", []byte("docx"))
	writeTestFile(t, root, "notes.txt", []byte("text"))
	writeTestFile(t, root, ".hidden.md", []byte("# hidden"))
	writeTestFile(t, root, "assets/image.png", []byte("png"))
	writeTestFile(t, root, "empty/.keep", []byte(""))

	server := newTestServer(t, root, nil)
	recorder := request(server, http.MethodGet, "/api/tree")
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", recorder.Code, recorder.Body.String())
	}

	response := decodeJSON[struct {
		Success bool        `json:"success"`
		Data    []TreeEntry `json:"data"`
	}](t, recorder)
	if !response.Success {
		t.Fatal("success 应为 true")
	}

	topNames := make([]string, 0, len(response.Data))
	for _, entry := range response.Data {
		topNames = append(topNames, entry.Name)
	}
	wantTop := []string{"files", "guides", "images", "office", "notes.txt"}
	if strings.Join(topNames, ",") != strings.Join(wantTop, ",") {
		t.Fatalf("顶层顺序 = %v，预期 %v", topNames, wantTop)
	}

	intro, ok := findTreeFile(response.Data, "guides/10-intro_20260101-abcdef.md")
	if !ok {
		t.Fatal("缺少 intro 文件")
	}
	if intro.DisplayName != "10-intro" {
		t.Fatalf("displayName = %q，预期 %q", intro.DisplayName, "10-intro")
	}
	if intro.FileType != "md" || intro.FileTypeCategory != "markdown" {
		t.Fatalf("文件类型 = %q/%q", intro.FileType, intro.FileTypeCategory)
	}

	guides, ok := findTreeFolder(response.Data, "guides")
	if !ok {
		t.Fatal("缺少 guides 目录")
	}
	guideNames := make([]string, 0, len(guides.Children))
	for _, entry := range guides.Children {
		guideNames = append(guideNames, entry.Name)
	}
	wantGuides := []string{"2-basics.md", "10-intro_20260101-abcdef.md", "demo.html"}
	if strings.Join(guideNames, ",") != strings.Join(wantGuides, ",") {
		t.Fatalf("guides 顺序 = %v，预期 %v", guideNames, wantGuides)
	}

	if _, ok := findTreeFile(response.Data, ".hidden.md"); ok {
		t.Fatal("应跳过隐藏文件")
	}
	if _, ok := findTreeFile(response.Data, "assets/image.png"); ok {
		t.Fatal("应跳过 assets 目录")
	}
	if _, ok := findTreeFolder(response.Data, "empty"); ok {
		t.Fatal("应跳过空目录")
	}
	if text, ok := findTreeFile(response.Data, "notes.txt"); !ok || text.FileTypeCategory != "text" {
		t.Fatalf("文本文件 = %+v，存在 = %v", text, ok)
	}
}

func TestTreeSkipsPathsRejectedByValidation(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "ok.md", []byte("# ok"))
	writeTestFile(t, root, "a:b.md", []byte("# colon"))
	writeTestFile(t, root, " 前导.md", []byte("# leading"))
	writeTestFile(t, root, "dir /inside.md", []byte("# trailing"))
	writeTestFile(t, root, "valid/inside.md", []byte("# inside"))

	server := newTestServer(t, root, nil)
	recorder := request(server, http.MethodGet, "/api/tree")
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeJSON[struct {
		Success bool        `json:"success"`
		Data    []TreeEntry `json:"data"`
	}](t, recorder)
	if !response.Success {
		t.Fatal("success 应为 true")
	}

	for _, invalid := range []string{"a:b.md", " 前导.md", "dir /inside.md"} {
		if _, ok := findTreeFile(response.Data, invalid); ok {
			t.Fatalf("不应列出无法访问的路径 %q", invalid)
		}
	}
	if _, ok := findTreeFolder(response.Data, "dir "); ok {
		t.Fatal("不应列出仅含无效文件的目录")
	}
	if _, ok := findTreeFile(response.Data, "ok.md"); !ok {
		t.Fatal("缺少正常文件 ok.md")
	}
	if _, ok := findTreeFile(response.Data, "valid/inside.md"); !ok {
		t.Fatal("缺少正常嵌套文件 valid/inside.md")
	}

	for _, invalid := range []string{"a:b.md", " 前导.md", "dir /inside.md"} {
		if status := request(server, http.MethodGet, "/file?path="+url.QueryEscape(invalid)).Code; status != http.StatusBadRequest {
			t.Fatalf("%q 访问状态码 = %d，预期 400", invalid, status)
		}
	}
}

func TestTreeAppliesSavedOrder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.md", []byte("a"))
	writeTestFile(t, root, "b.md", []byte("b"))
	writeTestFile(t, root, "guides/x.md", []byte("x"))
	writeTestFile(t, root, "guides/y.md", []byte("y"))
	writeTestFile(t, root, ".hzk-doc-order.json", []byte(`{"":["guides","b.md","a.md"],"guides":["y.md","x.md"]}`))

	server := newTestServer(t, root, nil)
	recorder := request(server, http.MethodGet, "/api/tree")
	response := decodeJSON[struct {
		Data []TreeEntry `json:"data"`
	}](t, recorder)

	topNames := make([]string, 0, len(response.Data))
	for _, entry := range response.Data {
		topNames = append(topNames, entry.Name)
	}
	if strings.Join(topNames, ",") != "guides,b.md,a.md" {
		t.Fatalf("排序后的顶层顺序 = %v", topNames)
	}

	guides, ok := findTreeFolder(response.Data, "guides")
	if !ok {
		t.Fatal("缺少 guides 目录")
	}
	guideNames := make([]string, 0, len(guides.Children))
	for _, entry := range guides.Children {
		guideNames = append(guideNames, entry.Name)
	}
	if strings.Join(guideNames, ",") != "y.md,x.md" {
		t.Fatalf("排序后的 guides 顺序 = %v", guideNames)
	}
}

func TestRefreshIfChangedAppliesSavedOrder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.md", []byte("a"))
	writeTestFile(t, root, "b.md", []byte("b"))
	server := newTestServer(t, root, nil)

	initial := request(server, http.MethodGet, "/api/tree")
	initialTree := decodeJSON[struct {
		Data []TreeEntry `json:"data"`
	}](t, initial)
	initialNames := make([]string, 0, len(initialTree.Data))
	for _, entry := range initialTree.Data {
		initialNames = append(initialNames, entry.Name)
	}
	if strings.Join(initialNames, ",") != "a.md,b.md" {
		t.Fatalf("初始顺序 = %v", initialNames)
	}

	writeTestFile(t, root, ".hzk-doc-order.json", []byte(`{"":["b.md","a.md"]}`))
	if err := server.refreshIfChanged(); err != nil {
		t.Fatalf("刷新排序失败: %v", err)
	}

	updated := request(server, http.MethodGet, "/api/tree")
	updatedTree := decodeJSON[struct {
		Data []TreeEntry `json:"data"`
	}](t, updated)
	updatedNames := make([]string, 0, len(updatedTree.Data))
	for _, entry := range updatedTree.Data {
		updatedNames = append(updatedNames, entry.Name)
	}
	if strings.Join(updatedNames, ",") != "b.md,a.md" {
		t.Fatalf("刷新后顺序 = %v", updatedNames)
	}
}

func TestEmptyDocsRoot(t *testing.T) {
	server := newTestServer(t, t.TempDir(), nil)

	tree := request(server, http.MethodGet, "/api/tree")
	treeResponse := decodeJSON[struct {
		Success bool        `json:"success"`
		Data    []TreeEntry `json:"data"`
	}](t, tree)
	if tree.Code != http.StatusOK || !treeResponse.Success || len(treeResponse.Data) != 0 {
		t.Fatalf("空目录树 = %d %+v", tree.Code, treeResponse)
	}

	site := request(server, http.MethodGet, "/api/site")
	siteResponse := decodeJSON[struct {
		Data siteConfig `json:"data"`
	}](t, site)
	if siteResponse.Data.Count != 0 {
		t.Fatalf("空库数量 = %d", siteResponse.Data.Count)
	}

	search := request(server, http.MethodGet, "/api/search?q=test&scope=full")
	searchResponse := decodeJSON[struct {
		Data []SearchResult `json:"data"`
	}](t, search)
	if search.Code != http.StatusOK || len(searchResponse.Data) != 0 {
		t.Fatalf("空库搜索 = %d %+v", search.Code, searchResponse.Data)
	}
}

func TestMarkdownRenderingRewritesResourcesAndKeepsCode(t *testing.T) {
	root := t.TempDir()
	source := "# 标题\n\n" +
		"![本地](assets/pic.png)\n\n" +
		"![相对](images/pic.png)\n\n" +
		"![内嵌](data:image/png;base64,iVBORw0KGgo=)\n\n" +
		"<video controls src=\"media/video.mp4\"></video>\n\n" +
		"[外链](https://example.com)\n\n" +
		"[另一篇](other.md)\n\n" +
		"[缺失](missing.md)\n\n" +
		"[锚点](#标题)\n\n" +
		"[恶意](javascript:alert(1))\n\n" +
		"<script>alert('x')</script>\n\n" +
		"```go\n  fmt.Println(\"x\")\n```\n"
	writeTestFile(t, root, "guides/intro.md", []byte(source))
	writeTestFile(t, root, "guides/other.md", []byte("# 另一篇"))
	writeTestFile(t, root, "assets/pic.png", []byte("png"))
	writeTestFile(t, root, "guides/images/pic.png", []byte("png"))
	writeTestFile(t, root, "guides/media/video.mp4", []byte("video"))

	server := newTestServer(t, root, nil)
	introID := documentIDFor(t, server, "guides/intro.md")
	otherID := documentIDFor(t, server, "guides/other.md")
	recorder := request(server, http.MethodGet, "/api/doc?id="+url.QueryEscape(introID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", recorder.Code, recorder.Body.String())
	}

	response := decodeJSON[struct {
		Success bool             `json:"success"`
		Data    documentResponse `json:"data"`
	}](t, recorder)
	if !response.Success || response.Data.Type != "markdown" {
		t.Fatalf("响应 = %+v", response)
	}
	if response.Data.ID != introID || response.Data.Path != "guides/intro.md" {
		t.Fatalf("文档标识 = %q/%q", response.Data.ID, response.Data.Path)
	}
	if response.Data.RawContent != source {
		t.Fatalf("rawContent 被修改")
	}

	html := response.Data.Content
	for _, want := range []string{
		"/file?path=assets%2Fpic.png",
		"/file?path=guides%2Fimages%2Fpic.png",
		"/file?path=guides%2Fmedia%2Fvideo.mp4",
		"/file?path=guides%2Fmissing.md",
		"data:image/png;base64,iVBORw0KGgo=",
		"/?id=" + url.QueryEscape(otherID),
		`target="_blank"`,
		`rel="noopener noreferrer"`,
		`href="#%E6%A0%87%E9%A2%98"`,
		"  fmt.Println",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("渲染结果缺少 %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "doc=guides") {
		t.Fatalf("未注册文档仍生成旧 ?doc 地址:\n%s", html)
	}
	if strings.Contains(html, "<script") || strings.Contains(html, "alert('x')") {
		t.Fatalf("脚本未被清洗:\n%s", html)
	}
	if strings.Contains(html, "javascript:") {
		t.Fatalf("javascript URL 未被清洗:\n%s", html)
	}
	if !strings.Contains(html, "<video") || !strings.Contains(html, "controls") {
		t.Fatalf("video 属性未被保留:\n%s", html)
	}
	if len(response.Data.Toc) != 1 {
		t.Fatalf("目录 = %+v", response.Data.Toc)
	}
	heading := response.Data.Toc[0]
	if heading.ID != "标题" || heading.Text != "标题" || heading.Level != 1 {
		t.Fatalf("目录标题 = %+v", heading)
	}
	if !strings.Contains(html, `id="标题"`) {
		t.Fatalf("渲染结果缺少标题 id:\n%s", html)
	}
	if response.Data.Size == 0 || response.Data.ModifiedAt == "" {
		t.Fatalf("文档元数据 = 大小 %d 修改时间 %q", response.Data.Size, response.Data.ModifiedAt)
	}
}

func TestMarkdownHeadingIDsSupportChinese(t *testing.T) {
	server := newTestServer(t, t.TempDir(), nil)
	rendered, err := server.PreviewMarkdown("a.md", "# 标题\n\n## 标题\n")
	if err != nil {
		t.Fatalf("PreviewMarkdown 失败: %v", err)
	}
	if !strings.Contains(rendered, `id="标题"`) || !strings.Contains(rendered, `id="标题-1"`) {
		t.Fatalf("标题 ID = %s", rendered)
	}
}

func TestPreviewMarkdownUsesDocumentDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "assets/pic.png", []byte("png"))
	writeTestFile(t, root, "guides/other.md", []byte("# 其它"))
	writeTestFile(t, root, "guides/demo.html", []byte("<html></html>"))
	writeTestFile(t, root, "guides/manual.pdf", []byte("%PDF-1.4"))

	server := newTestServer(t, root, nil)
	ids := treeDocumentIDs(t, server)
	rendered, err := server.PreviewMarkdown("guides/page.md", "![图片](assets/pic.png)\n\n[文档](other.md)\n\n[网页](demo.html)\n\n[文件](manual.pdf)\n\n[缺失](missing.md)\n")
	if err != nil {
		t.Fatalf("PreviewMarkdown 失败: %v", err)
	}
	if !strings.Contains(rendered, "/file?path=assets%2Fpic.png") {
		t.Fatalf("预览未解析 assets: %s", rendered)
	}
	if !strings.Contains(rendered, "/file?path=guides%2Fmissing.md") || strings.Contains(rendered, "?doc=") {
		t.Fatalf("预览未按真实路径处理缺失文档: %s", rendered)
	}
	for _, name := range []string{"guides/other.md", "guides/demo.html", "guides/manual.pdf"} {
		if ids[name] == "" {
			t.Fatalf("%s 缺少文档 ID", name)
		}
		if !strings.Contains(rendered, "/?id="+ids[name]) {
			t.Errorf("预览链接缺少 %s 的 ID: %s", name, rendered)
		}
	}
}

func TestHTMLViewerRewritesAssetsAndSetsCSP(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "demo.html", []byte(`<!doctype html>
<html><head><link rel="stylesheet" href="assets/site.css"><script>alert(1)</script></head>
<body onload="alert(2)"><form action="/steal"><input name="x"></form>
<img src="/assets/pic.png" onerror="alert(3)">
<a href="https://example.com" target="_top">external</a>
<a href="other.html">other</a>
<a href="javascript:alert(4)">bad</a>
<a href="#local">local</a></body></html>`))
	writeTestFile(t, root, "assets/site.css", []byte("body{}"))
	writeTestFile(t, root, "assets/pic.png", []byte("png"))

	server := newTestServer(t, root, nil)
	recorder := request(server, http.MethodGet, "/view/html?path=demo.html")
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"/file?path=assets%2Fsite.css",
		"/file?path=assets%2Fpic.png",
		"/view/html?path=other.html",
		`target="_blank"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("HTML 响应缺少 %q:\n%s", want, body)
		}
	}
	if csp := recorder.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Fatalf("CSP = %q", csp)
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("缺少 nosniff 响应头")
	}
	for _, forbidden := range []string{"<script", "<form", "<input", "alert(", "onload", "onerror", `target="_top"`, "javascript:"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("HTML 响应仍包含 %q:\n%s", forbidden, body)
		}
	}
	if !strings.Contains(body, `href="#local"`) {
		t.Errorf("页内锚点被改写:\n%s", body)
	}
}

func TestHTMLViewerRewritesSrcsetAndNormalizesSchemes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "demo.html", []byte(`<html><body>
<img src="img/hero.png" srcset="img/hero.png 1x, img/hero@2x.png 2x, assets/banner.png 480w, https://cdn.example.com/remote.png 3x, data:image/png;base64,iVBORw0KGgo= 4x, javascript:alert(5) 5x">
<picture><source srcset="img/wide.png 800w, img/narrow.png 320w"></picture>
<a href="java&#10;script:alert(1)">bad1</a>
<a href="jav&#9;ascript:alert(2)">bad2</a>
<a href="https://example.com/path">good</a>
</body></html>`))

	server := newTestServer(t, root, nil)
	recorder := request(server, http.MethodGet, "/view/html?path=demo.html")
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`srcset="/file?path=img%2Fhero.png 1x, /file?path=img%2Fhero%402x.png 2x, /file?path=assets%2Fbanner.png 480w, https://cdn.example.com/remote.png 3x, data:image/png;base64,iVBORw0KGgo= 4x, # 5x"`,
		`<source srcset="/file?path=img%2Fwide.png 800w, /file?path=img%2Fnarrow.png 320w"/>`,
		`href="https://example.com/path"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("HTML 响应缺少 %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"javascript:", "java\nscript:", "jav\tascript:"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("HTML 响应仍包含 %q:\n%s", forbidden, body)
		}
	}
	if csp := recorder.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Fatalf("CSP = %q", csp)
	}
}

func TestPathValidationAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "guides/ok.md", []byte("# ok"))
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("# outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.md")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	outsidePNG := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outsidePNG, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePNG, filepath.Join(root, "assets", "link.png")); err != nil {
		_ = os.MkdirAll(filepath.Join(root, "assets"), 0o755)
		if err := os.Symlink(outsidePNG, filepath.Join(root, "assets", "link.png")); err != nil {
			t.Skipf("无法创建资源符号链接: %v", err)
		}
	}
	writeTestFile(t, root, ".hzk-doc-order.json", []byte(`{"":["ok"]}`))
	writeTestFile(t, root, ".secret.html", []byte("<html>hidden</html>"))
	if err := os.Symlink(filepath.Join(root, ".hzk-doc-order.json"), filepath.Join(root, "public.md")); err != nil {
		t.Skipf("无法创建元数据符号链接: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, ".secret.html"), filepath.Join(root, "public.html")); err != nil {
		t.Skipf("无法创建 HTML 元数据符号链接: %v", err)
	}
	writeTestFile(t, root, ".hidden-dir/secret.pdf", []byte("%PDF-1.4"))
	if err := os.Symlink(filepath.Join(root, ".hidden-dir"), filepath.Join(root, "public-dir")); err != nil {
		t.Skipf("无法创建目录符号链接: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "folder.pdf"), 0o755); err != nil {
		t.Fatal(err)
	}

	server := newTestServer(t, root, nil)
	for _, target := range []string{
		"/file?path=../outside.md",
		"/file?path=%2e%2e%2Foutside.md",
		"/file?path=/etc/passwd",
		"/file?path=guides/../../outside.md",
		"/file?path=a/../b.md",
		"/file?path=.hidden.md",
		"/file?path=guides/.hidden.md",
		"/file?path=a//b.md",
		"/file?path=a%5Cb.md",
		"/file?path=../outside.png",
		"/view/html?path=../outside.html",
	} {
		recorder := request(server, http.MethodGet, target)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: 状态码 = %d，预期 400；响应 = %s", target, recorder.Code, recorder.Body.String())
		}
	}

	for _, target := range []string{
		"/api/doc?path=../outside.md",
		"/api/doc?path=guides/ok.md",
		"/api/doc?path=.hidden.md",
	} {
		if recorder := request(server, http.MethodGet, target); recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: 状态码 = %d，预期 400；响应 = %s", target, recorder.Code, recorder.Body.String())
		}
	}

	if recorder := request(server, http.MethodGet, "/file?path=link.md"); recorder.Code != http.StatusNotFound {
		t.Fatalf("文档符号链接状态码 = %d，预期 404", recorder.Code)
	}
	if recorder := request(server, http.MethodGet, "/file?path=assets/link.png"); recorder.Code != http.StatusNotFound {
		t.Fatalf("资源符号链接状态码 = %d，预期 404", recorder.Code)
	}
	if recorder := request(server, http.MethodGet, "/file?path=public.md"); recorder.Code != http.StatusNotFound {
		t.Fatalf("元数据符号链接状态码 = %d，预期 404", recorder.Code)
	}
	if recorder := request(server, http.MethodGet, "/view/html?path=public.html"); recorder.Code != http.StatusNotFound {
		t.Fatalf("HTML 符号链接状态码 = %d，预期 404", recorder.Code)
	}
	if recorder := request(server, http.MethodGet, "/file?path=public-dir/secret.pdf"); recorder.Code != http.StatusNotFound {
		t.Fatalf("目录符号链接状态码 = %d，预期 404", recorder.Code)
	}
	if recorder := request(server, http.MethodGet, "/file?path=folder.pdf"); recorder.Code != http.StatusNotFound {
		t.Fatalf("非普通文件状态码 = %d，预期 404", recorder.Code)
	}
	if recorder := request(server, http.MethodGet, "/api/doc?id="+url.QueryEscape(documentIDFor(t, server, "guides/ok.md"))); recorder.Code != http.StatusOK {
		t.Fatalf("有效文档状态码 = %d", recorder.Code)
	}
}

func TestFileRangeAndMIME(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "assets/data.png", []byte("0123456789"))
	writeTestFile(t, root, "assets/style.css", []byte("body{}"))
	writeTestFile(t, root, "assets/app.js", []byte("alert(1)"))
	writeTestFile(t, root, "assets/icon.svg", []byte("<svg></svg>"))

	server := newTestServer(t, root, nil)
	recorder := request(server, http.MethodGet, "/file?path=assets/data.png")
	if recorder.Code != http.StatusOK || recorder.Body.String() != "0123456789" {
		t.Fatalf("资源响应 = %d %q", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("内容类型 = %q", contentType)
	}

	rangeRequest := httptest.NewRequest(http.MethodGet, "/file?path=assets/data.png", nil)
	rangeRequest.Header.Set("Range", "bytes=2-4")
	rangeRecorder := httptest.NewRecorder()
	server.ServeHTTP(rangeRecorder, rangeRequest)
	if rangeRecorder.Code != http.StatusPartialContent {
		t.Fatalf("范围请求状态码 = %d", rangeRecorder.Code)
	}
	if body := rangeRecorder.Body.String(); body != "234" {
		t.Fatalf("范围响应 = %q", body)
	}
	if contentRange := rangeRecorder.Header().Get("Content-Range"); contentRange != "bytes 2-4/10" {
		t.Fatalf("内容范围 = %q", contentRange)
	}

	if recorder := request(server, http.MethodGet, "/file?path=assets/style.css"); recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("CSS 响应 = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	if recorder := request(server, http.MethodGet, "/file?path=assets/app.js"); recorder.Code != http.StatusForbidden {
		t.Fatalf("JS 状态码 = %d，预期 403", recorder.Code)
	}
	svg := request(server, http.MethodGet, "/file?path=assets/icon.svg")
	if svg.Code != http.StatusOK || !strings.Contains(svg.Header().Get("Content-Security-Policy"), "sandbox") || !strings.Contains(svg.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("SVG 响应 = %d，CSP = %q", svg.Code, svg.Header().Get("Content-Security-Policy"))
	}
}

func TestFileResponses(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "files/a.pdf", []byte("%PDF-1.4"))
	writeTestFile(t, root, "files/a.docx", []byte("docx"))
	writeTestFile(t, root, "files/a.txt", []byte("text"))
	writeTestFile(t, root, "files/a.md", []byte("# 文档"))
	writeTestFile(t, root, "files/a.html", []byte("<html><body>html</body></html>"))
	writeTestFile(t, root, "images/pic.png", []byte("png"))
	writeTestFile(t, root, "media/clip.mp4", []byte("mp4"))
	writeTestFile(t, root, "media/sound.mp3", []byte("mp3"))

	server := newTestServer(t, root, nil)
	ids := treeDocumentIDs(t, server)
	cases := []struct {
		path     string
		wantType string
		wantURL  string
	}{
		{"files/a.pdf", "pdf", "/file?id="},
		{"files/a.docx", "docx", "/file?id="},
		{"files/a.txt", "text", "/file?id="},
		{"files/a.html", "html", "/view/html?id="},
		{"images/pic.png", "image", "/file?id="},
		{"media/clip.mp4", "video", "/file?id="},
		{"media/sound.mp3", "audio", "/file?id="},
	}
	for _, tc := range cases {
		id := ids[tc.path]
		if id == "" {
			t.Fatalf("%s 缺少文档 ID", tc.path)
		}
		recorder := request(server, http.MethodGet, "/api/doc?id="+url.QueryEscape(id))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d，响应 = %s", tc.path, recorder.Code, recorder.Body.String())
		}
		response := decodeJSON[struct {
			Success bool             `json:"success"`
			Data    documentResponse `json:"data"`
		}](t, recorder)
		wantURL := tc.wantURL + url.QueryEscape(id)
		if response.Data.Type != tc.wantType || response.Data.URL != wantURL || response.Data.ID != id {
			t.Errorf("%s = 类型 %q URL %q ID %q，预期 %q %q %q", tc.path, response.Data.Type, response.Data.URL, response.Data.ID, tc.wantType, wantURL, id)
		}
		if response.Data.Size == 0 || response.Data.ModifiedAt == "" || response.Data.Toc == nil {
			t.Errorf("%s 缺少 toc/size/modifiedAt: %+v", tc.path, response.Data)
		}
		if tc.path == "files/a.txt" && response.Data.RawContent != "text" {
			t.Errorf("txt rawContent = %q，预期 %q", response.Data.RawContent, "text")
		}
	}

	file := request(server, http.MethodGet, "/file?path=files/a.docx")
	if file.Code != http.StatusOK || file.Body.String() != "docx" {
		t.Fatalf("文件响应 = %d %q", file.Code, file.Body.String())
	}
	if contentType := file.Header().Get("Content-Type"); contentType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("文件内容类型 = %q", contentType)
	}
	if disposition := file.Header().Get("Content-Disposition"); !strings.Contains(disposition, "inline") {
		t.Fatalf("处置方式 = %q", disposition)
	}
	download := request(server, http.MethodGet, "/file?path=files/a.docx&download=1")
	if disposition := download.Header().Get("Content-Disposition"); !strings.Contains(disposition, "attachment") {
		t.Fatalf("下载处置方式 = %q", disposition)
	}
	if text := request(server, http.MethodGet, "/file?path=files/a.txt"); text.Code != http.StatusOK || text.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("文本响应 = %d %q", text.Code, text.Header().Get("Content-Type"))
	}
	markdown := request(server, http.MethodGet, "/file?path=files/a.md&download=1")
	if markdown.Code != http.StatusOK || markdown.Header().Get("Content-Type") != "text/markdown; charset=utf-8" || !strings.Contains(markdown.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("Markdown 文件 = %d %q %q", markdown.Code, markdown.Header().Get("Content-Type"), markdown.Header().Get("Content-Disposition"))
	}
	htmlFile := request(server, http.MethodGet, "/file?path=files/a.html")
	if htmlFile.Code != http.StatusOK || htmlFile.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("HTML 文件 = %d %q", htmlFile.Code, htmlFile.Header().Get("Content-Type"))
	}
	if csp := htmlFile.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Fatalf("HTML 文件 CSP = %q", csp)
	}
	if image := request(server, http.MethodGet, "/file?path=images/pic.png"); image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/png" || image.Body.String() != "png" {
		t.Fatalf("通过 /file 访问图片 = %d %q %q", image.Code, image.Header().Get("Content-Type"), image.Body.String())
	}
	head := request(server, http.MethodHead, "/file?path=files/a.pdf")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("HEAD 文件 = %d %q %q", head.Code, head.Header().Get("Content-Type"), head.Body.String())
	}
	if forbidden := request(server, http.MethodGet, "/file?path=assets/app.js"); forbidden.Code != http.StatusForbidden {
		t.Fatalf("JS 文件状态码 = %d，预期 403", forbidden.Code)
	}
	for _, documentPath := range []string{
		"files/a.pdf", "files/a.docx", "files/a.txt", "files/a.md", "files/a.html",
		"images/pic.png", "media/clip.mp4", "media/sound.mp3",
	} {
		response := request(server, http.MethodGet, "/file?path="+url.QueryEscape(documentPath)+"&download=1")
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Disposition"), "attachment") {
			t.Errorf("%s 下载响应 = %d %q", documentPath, response.Code, response.Header().Get("Content-Disposition"))
		}
	}
}

func findTreeFolder(entries []TreeEntry, path string) (TreeEntry, bool) {
	for _, entry := range entries {
		if entry.Type == "folder" && entry.Path == path {
			return entry, true
		}
		if entry.Type == "folder" {
			if found, ok := findTreeFolder(entry.Children, path); ok {
				return found, true
			}
		}
	}
	return TreeEntry{}, false
}

func TestDocumentQueryUsesIDs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "guides/intro.md", []byte("# 指南\n\n接口关键字"))
	writeTestFile(t, root, "guides/demo.html", []byte("<html><body>演示</body></html>"))
	writeTestFile(t, root, "notes/plain.txt", []byte("文本"))
	writeTestFile(t, root, "images/pic.png", []byte("png"))

	server := newTestServer(t, root, nil)
	ids := treeDocumentIDs(t, server)
	for _, name := range []string{"guides/intro.md", "guides/demo.html", "notes/plain.txt", "images/pic.png"} {
		if ids[name] == "" {
			t.Fatalf("%s 缺少文档 ID", name)
		}
	}
	if len(ids) != 4 {
		t.Fatalf("文档数量 = %d，预期 4: %v", len(ids), ids)
	}

	recorder := request(server, http.MethodGet, "/api/doc?id="+url.QueryEscape(ids["guides/intro.md"]))
	response := decodeJSON[struct {
		Data documentResponse `json:"data"`
	}](t, recorder)
	if recorder.Code != http.StatusOK || response.Data.ID != ids["guides/intro.md"] || response.Data.Path != "guides/intro.md" {
		t.Fatalf("ID 读取 = %d %+v", recorder.Code, response.Data)
	}

	search := request(server, http.MethodGet, "/api/search?q="+url.QueryEscape("接口关键字")+"&scope=full")
	results := decodeJSON[struct {
		Data []SearchResult `json:"data"`
	}](t, search)
	if len(results.Data) != 1 || results.Data[0].ID != ids["guides/intro.md"] {
		t.Fatalf("搜索结果 = %+v", results.Data)
	}

	if recorder := request(server, http.MethodGet, "/file?id="+url.QueryEscape(ids["images/pic.png"])); recorder.Code != http.StatusOK || recorder.Body.String() != "png" {
		t.Fatalf("按 ID 读取文件 = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder := request(server, http.MethodGet, "/view/html?id="+url.QueryEscape(ids["guides/demo.html"])); recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "演示") {
		t.Fatalf("按 ID 查看 HTML = %d %q", recorder.Code, recorder.Body.String())
	}

	if status := request(server, http.MethodGet, "/api/doc").Code; status != http.StatusBadRequest {
		t.Fatalf("缺少 ID 状态码 = %d，预期 400", status)
	}
	unknownID := strings.Repeat("0", 32)
	if status := request(server, http.MethodGet, "/api/doc?id="+unknownID).Code; status != http.StatusNotFound {
		t.Fatalf("未知 ID 状态码 = %d，预期 404", status)
	}
	if status := request(server, http.MethodGet, "/api/doc?path=guides/intro.md").Code; status != http.StatusBadRequest {
		t.Fatalf("仅带路径状态码 = %d，预期 400", status)
	}
	if status := request(server, http.MethodGet, "/file?id="+unknownID).Code; status != http.StatusNotFound {
		t.Fatalf("未知文件 ID 状态码 = %d，预期 404", status)
	}
	for _, invalid := range []string{"not-a-valid-id", "../outside.md", "guides/.hidden.md", "guides/intro.md"} {
		if status := request(server, http.MethodGet, "/api/doc?id="+url.QueryEscape(invalid)).Code; status != http.StatusNotFound {
			t.Errorf("/api/doc?id=%s 状态码 = %d，预期 404", invalid, status)
		}
	}

	current := server.current()
	if current == nil || current.docs["guides/intro.md"].ID != ids["guides/intro.md"] || current.byID[ids["guides/intro.md"]].Path != "guides/intro.md" {
		t.Fatalf("索引中的 ID 与目录不一致: %+v", current)
	}
}
