package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type searchAPIResponse struct {
	Success bool           `json:"success"`
	Data    []SearchResult `json:"data"`
}

func searchRequest(t *testing.T, server *Server, query, scope string) *searchAPIResponse {
	t.Helper()
	target := "/api/search?q=" + url.QueryEscape(query)
	if scope != "" {
		target += "&scope=" + url.QueryEscape(scope)
	}
	recorder := request(server, http.MethodGet, target)
	if recorder.Code != http.StatusOK {
		t.Fatalf("搜索 %q/%q 状态码 = %d，响应 = %s", query, scope, recorder.Code, recorder.Body.String())
	}
	response := decodeJSON[searchAPIResponse](t, recorder)
	return &response
}

func resultPaths(results []SearchResult) []string {
	paths := make([]string, 0, len(results))
	for _, result := range results {
		paths = append(paths, result.Path)
	}
	return paths
}

func containsPath(results []SearchResult, path string) bool {
	for _, result := range results {
		if result.Path == path {
			return true
		}
	}
	return false
}

func TestSearchTitleAndFullAcrossFormats(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "guides/部署说明.md", []byte("# 部署说明\n\n部署前先检查环境。"))
	writeTestFile(t, root, "guides/发布流程.md", []byte("# 发布流程\n\n上线前检查。"))
	writeTestFile(t, root, "notes/部署记录.txt", []byte("部署记录"))
	writeTestFile(t, root, "files/手册.pdf", []byte("%PDF-1.4"))
	writeTestFile(t, root, "office/plan.docx", []byte("docx"))

	server := newTestServerWithOptions(t, root, nil, func(options *newOptions) {
		options.pdfExtractor = func(path string) (string, error) {
			return "PDF 部署正文", nil
		}
	})

	title := searchRequest(t, server, "部署", "title")
	if !containsPath(title.Data, "guides/部署说明.md") || !containsPath(title.Data, "notes/部署记录.txt") {
		t.Fatalf("标题搜索结果 = %v", resultPaths(title.Data))
	}
	if containsPath(title.Data, "files/手册.pdf") {
		t.Fatal("PDF 标题不应匹配“部署”")
	}

	full := searchRequest(t, server, "部署", "full")
	for _, path := range []string{"guides/部署说明.md", "notes/部署记录.txt", "files/手册.pdf"} {
		if !containsPath(full.Data, path) {
			t.Errorf("全文搜索缺少 %s: %v", path, resultPaths(full.Data))
		}
	}
	if len(full.Data) == 0 || full.Data[0].Path != "guides/部署说明.md" {
		t.Fatalf("标题命中应排在最前: %v", resultPaths(full.Data))
	}
	foundSnippet := false
	for _, result := range full.Data {
		if strings.Contains(result.Snippet, "[[部署]]") {
			foundSnippet = true
		}
	}
	if !foundSnippet {
		t.Fatalf("没有高亮片段: %+v", full.Data)
	}

	if results := searchRequest(t, server, "部", "full"); !containsPath(results.Data, "guides/部署说明.md") {
		t.Fatalf("单字搜索结果 = %v", resultPaths(results.Data))
	}
	if results := searchRequest(t, server, "部署", "full"); len(results.Data) != 3 {
		t.Fatalf("双字搜索结果 = %v", resultPaths(results.Data))
	}
	if results := searchRequest(t, server, "部署前", "full"); len(results.Data) != 1 || results.Data[0].Path != "guides/部署说明.md" {
		t.Fatalf("三字搜索结果 = %v", resultPaths(results.Data))
	}
	if results := searchRequest(t, server, "plan", "title"); len(results.Data) != 1 || results.Data[0].Path != "office/plan.docx" {
		t.Fatalf("Office 标题搜索结果 = %v", resultPaths(results.Data))
	}
	if results := searchRequest(t, server, "不存在", "full"); len(results.Data) != 0 {
		t.Fatalf("意外搜索结果 = %v", resultPaths(results.Data))
	}
}

func TestSearchSnippetHandlesUnicodeCaseMapping(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "minimal.md", []byte("# 最小\n\nȺ a"))
	writeTestFile(t, root, "组合.md", []byte("# 组合\n\n中文搜索 café İstanbul"))
	server := newTestServer(t, root, nil)

	recorder := request(server, http.MethodGet, "/api/search?q=a&scope=full")
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeJSON[searchAPIResponse](t, recorder)
	var minimal *SearchResult
	for index := range response.Data {
		if response.Data[index].Path == "minimal.md" {
			minimal = &response.Data[index]
		}
	}
	if minimal == nil || !strings.Contains(minimal.Snippet, "Ⱥ [[a]]") {
		t.Fatalf("最小用例片段 = %+v", response.Data)
	}

	chinese := searchRequest(t, server, "中文", "full")
	if len(chinese.Data) != 1 || !strings.Contains(chinese.Data[0].Snippet, "[[中文]]搜索") {
		t.Fatalf("中文片段 = %+v", chinese.Data)
	}
	upper := searchRequest(t, server, "CAFÉ", "full")
	if len(upper.Data) != 1 || !strings.Contains(upper.Data[0].Snippet, "[[café]]") {
		t.Fatalf("大写查询片段 = %+v", upper.Data)
	}
	istanbul := searchRequest(t, server, "İstanbul", "full")
	if len(istanbul.Data) != 1 || !strings.Contains(istanbul.Data[0].Snippet, "[[İstanbul]]") {
		t.Fatalf("İstanbul 片段 = %+v", istanbul.Data)
	}
}

func TestSearchValidationAndEmptyQuery(t *testing.T) {
	server := newTestServer(t, t.TempDir(), nil)

	empty := searchRequest(t, server, "", "title")
	if len(empty.Data) != 0 {
		t.Fatalf("空查询结果 = %v", resultPaths(empty.Data))
	}

	longQuery := strings.Repeat("长", 101)
	longRecorder := request(server, http.MethodGet, "/api/search?q="+url.QueryEscape(longQuery)+"&scope=title")
	if longRecorder.Code != http.StatusBadRequest {
		t.Fatalf("超长查询状态码 = %d", longRecorder.Code)
	}

	badScope := request(server, http.MethodGet, "/api/search?q=test&scope=unknown")
	if badScope.Code != http.StatusBadRequest {
		t.Fatalf("非法搜索范围状态码 = %d", badScope.Code)
	}

	badScopeEmpty := request(server, http.MethodGet, "/api/search?scope=unknown")
	if badScopeEmpty.Code != http.StatusBadRequest {
		t.Fatalf("空查询搭配非法搜索范围状态码 = %d", badScopeEmpty.Code)
	}
}

func TestSearchLimit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 35; i++ {
		writeTestFile(t, root, "docs/doc-"+twoDigits(i)+".md", []byte("关键词内容"))
	}
	server := newTestServer(t, root, nil)
	results := searchRequest(t, server, "关键词", "full")
	if len(results.Data) != 30 {
		t.Fatalf("结果数量 = %d，预期 30", len(results.Data))
	}
}

func TestRefreshIfChangedSkipsUnchangedDocuments(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "manual.pdf", []byte("%PDF-1.4"))
	calls := 0
	server := newTestServerWithOptions(t, root, nil, func(options *newOptions) {
		options.pdfExtractor = func(path string) (string, error) {
			calls++
			return "PDF 正文", nil
		}
	})
	if calls != 1 {
		t.Fatalf("初始提取次数 = %d，预期 1", calls)
	}
	if err := server.refreshIfChanged(); err != nil {
		t.Fatalf("refreshIfChanged 失败: %v", err)
	}
	if calls != 1 {
		t.Fatalf("未变更时提取次数 = %d，预期 1", calls)
	}
	writeTestFile(t, root, "new.md", []byte("新增"))
	if err := server.refreshIfChanged(); err != nil {
		t.Fatalf("变更后 refreshIfChanged 失败: %v", err)
	}
	if calls != 2 {
		t.Fatalf("变更后提取次数 = %d，预期 2", calls)
	}
}

func TestRefreshPicksUpNewDocuments(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "first.md", []byte("初始内容"))
	server := newTestServer(t, root, nil)

	if results := searchRequest(t, server, "新增", "full"); len(results.Data) != 0 {
		t.Fatalf("初始搜索不应有结果 = %v", resultPaths(results.Data))
	}

	writeTestFile(t, root, "second.md", []byte("新增内容"))
	if err := server.Refresh(); err != nil {
		t.Fatalf("Refresh 失败: %v", err)
	}

	results := searchRequest(t, server, "新增", "full")
	if len(results.Data) != 1 || results.Data[0].Path != "second.md" {
		t.Fatalf("刷新后搜索结果 = %v", resultPaths(results.Data))
	}
	tree := request(server, http.MethodGet, "/api/tree")
	if !strings.Contains(tree.Body.String(), "second.md") {
		t.Fatalf("刷新后目录树 = %s", tree.Body.String())
	}
}

func TestPureGoPDFTextExtractionAndSearch(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "manual.pdf", minimalTextPDF("Deployment Guide"))

	file, err := os.Open(filepath.Join(root, "manual.pdf"))
	if err != nil {
		t.Fatalf("打开 PDF 失败: %v", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("读取 PDF 信息失败: %v", err)
	}
	text, err := extractPDFPureGo(file, info.Size())
	if err != nil {
		t.Fatalf("纯 Go PDF 提取失败: %v", err)
	}
	if !strings.Contains(text, "Deployment Guide") {
		t.Fatalf("PDF 文本 = %q", text)
	}

	server := newTestServer(t, root, nil)
	results := searchRequest(t, server, "Deployment", "full")
	if len(results.Data) != 1 || results.Data[0].Path != "manual.pdf" {
		t.Fatalf("PDF 搜索结果 = %v", resultPaths(results.Data))
	}
}

func TestPDFTextFallbackReadsStandardInput(t *testing.T) {
	binary, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("未安装 pdftotext")
	}
	root := t.TempDir()
	writeTestFile(t, root, "manual.pdf", minimalTextPDF("Deployment Guide"))
	file, err := os.Open(filepath.Join(root, "manual.pdf"))
	if err != nil {
		t.Fatalf("打开 PDF 失败: %v", err)
	}
	defer file.Close()

	text, err := extractPDFText(binary, file)
	if err != nil {
		t.Fatalf("pdftotext 提取失败: %v", err)
	}
	if !strings.Contains(text, "Deployment Guide") {
		t.Fatalf("pdftotext 输出 = %q", text)
	}
}

func minimalTextPDF(text string) []byte {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 6)
	writeObject := func(number int, body string) {
		offsets[number] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", number, body)
	}

	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 144] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>")
	stream := fmt.Sprintf("BT /F1 24 Tf 20 100 Td (%s) Tj ET", text)
	writeObject(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream)+1, stream))
	writeObject(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xrefOffset := pdf.Len()
	pdf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for number := 1; number <= 5; number++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[number])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return pdf.Bytes()
}

func twoDigits(value int) string {
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
