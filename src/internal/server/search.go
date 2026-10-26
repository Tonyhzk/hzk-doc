package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
)

var (
	errQueryTooLong = errors.New("搜索内容过长")
	errInvalidScope = errors.New("无效的搜索范围")
)

type SearchResult struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Extension string `json:"extension"`
	Snippet   string `json:"snippet"`

	score int
}

type searchDoc struct {
	meta    DocMeta
	title   string
	content string
}

type posting struct {
	doc     int
	title   int
	content int
}

type searchIndex struct {
	docs   []searchDoc
	tokens map[string][]posting
}

func buildSearchIndex(server *Server, docs map[string]DocMeta) *searchIndex {
	paths := make([]string, 0, len(docs))
	for path := range docs {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	index := &searchIndex{
		docs:   make([]searchDoc, 0, len(paths)),
		tokens: make(map[string][]posting),
	}

	for _, path := range paths {
		meta := docs[path]
		content := server.extractSearchContent(meta)
		doc := searchDoc{
			meta:    meta,
			title:   normalizeSearchText(meta.Title),
			content: content,
		}
		docID := len(index.docs)
		index.docs = append(index.docs, doc)

		titleTokens := tokenize(doc.title)
		contentTokens := tokenize(doc.content)
		combined := make(map[string]posting, len(titleTokens)+len(contentTokens))
		for token, count := range titleTokens {
			entry := combined[token]
			entry.title = count
			combined[token] = entry
		}
		for token, count := range contentTokens {
			entry := combined[token]
			entry.content = count
			combined[token] = entry
		}
		for token, entry := range combined {
			entry.doc = docID
			index.tokens[token] = append(index.tokens[token], entry)
		}
	}

	return index
}

func (s *Server) extractSearchContent(meta DocMeta) string {
	switch meta.Extension {
	case "md", "markdown", "mark":
		source, err := s.readDocument(meta.Path)
		if err != nil {
			s.logger.Warn("跳过 Markdown 搜索内容", "path", meta.Path, "error", err)
			return ""
		}
		return normalizeSearchText(string(source))
	case "html", "htm":
		source, err := s.readDocument(meta.Path)
		if err != nil {
			s.logger.Warn("跳过 HTML 搜索内容", "path", meta.Path, "error", err)
			return ""
		}
		return extractHTMLText(string(source))
	case "txt":
		source, err := s.readDocument(meta.Path)
		if err != nil {
			s.logger.Warn("跳过文本搜索内容", "path", meta.Path, "error", err)
			return ""
		}
		return normalizeSearchText(string(source))
	case "pdf":
		if s.pdfExtract == nil {
			return ""
		}
		text, err := s.pdfExtract(meta.Path)
		if err != nil {
			s.logger.Warn("跳过 PDF 搜索内容", "path", meta.Path, "error", err)
			return ""
		}
		return normalizeSearchText(text)
	default:
		return ""
	}
}

func extractPDFPureGo(file *os.File, size int64) (text string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text = ""
			err = fmt.Errorf("PDF 提取异常: %v", recovered)
		}
	}()

	reader, err := pdf.NewReader(file, size)
	if err != nil {
		return "", err
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(plain)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func extractPDFText(binary string, file *os.File) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, binary, "-enc", "UTF-8", "-nopgbrk", "-", "-")
	command.Stdin = file
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func extractHTMLText(source string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(source))
	ignored := map[string]bool{
		"script": true, "style": true, "template": true, "noscript": true,
		"pre": true, "code": true, "svg": true, "canvas": true,
	}
	skipDepth := 0
	parts := make([]string, 0, 64)

	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return normalizeSearchText(strings.Join(parts, " "))
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			if ignored[string(name)] {
				skipDepth++
			}
		case html.SelfClosingTagToken:
			// 自闭合的忽略标签不会开启跳过范围。
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if ignored[string(name)] && skipDepth > 0 {
				skipDepth--
			}
		case html.TextToken:
			if skipDepth == 0 {
				parts = append(parts, string(tokenizer.Text()))
			}
		}
	}
}

func normalizeSearchText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	return strings.Join(strings.Fields(value), " ")
}

// 中日韩文本按二元组索引，单字查询回退到线性扫描。
func tokenize(value string) map[string]int {
	tokens := make(map[string]int)
	runes := []rune(strings.ToLower(value))

	for i := 0; i < len(runes); {
		if isCJK(runes[i]) {
			start := i
			for i < len(runes) && isCJK(runes[i]) {
				i++
			}
			for j := start; j+1 < i; j++ {
				tokens[string(runes[j:j+2])]++
			}
			continue
		}

		if unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) {
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
				i++
			}
			tokens[string(runes[start:i])]++
			continue
		}
		i++
	}

	return tokens
}

func isCJK(value rune) bool {
	return unicode.Is(unicode.Han, value) ||
		unicode.Is(unicode.Hiragana, value) ||
		unicode.Is(unicode.Katakana, value) ||
		unicode.Is(unicode.Hangul, value)
}

func (index *searchIndex) search(rawQuery, rawScope string) ([]SearchResult, error) {
	scope := strings.ToLower(strings.TrimSpace(rawScope))
	if scope == "" {
		scope = "title"
	}
	if scope != "title" && scope != "full" {
		return nil, errInvalidScope
	}

	query := normalizeSearchText(rawQuery)
	if query == "" {
		return []SearchResult{}, nil
	}
	if utf8.RuneCountInString(query) > 100 {
		return nil, errQueryTooLong
	}

	tokens := tokenize(query)
	candidates := index.candidates(tokens)
	results := make([]SearchResult, 0)

	for _, docID := range candidates {
		doc := index.docs[docID]
		titleCount := countOccurrences(doc.title, query)
		contentCount := 0
		if scope == "full" {
			contentCount = countOccurrences(doc.content, query)
		}

		if scope == "title" && titleCount == 0 {
			continue
		}
		if scope == "full" && titleCount == 0 && contentCount == 0 {
			continue
		}

		score := 8*titleCount + contentCount
		snippet := ""
		if scope == "full" && contentCount > 0 {
			snippet = makeSnippet(doc.content, query)
		}

		results = append(results, SearchResult{
			ID:        doc.meta.ID,
			Path:      doc.meta.Path,
			Title:     doc.meta.Title,
			Directory: doc.meta.Directory,
			Extension: doc.meta.Extension,
			Snippet:   snippet,
			score:     score,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		if results[i].Title != results[j].Title {
			return results[i].Title < results[j].Title
		}
		return results[i].Path < results[j].Path
	})

	if len(results) > 30 {
		results = results[:30]
	}
	return results, nil
}

func (index *searchIndex) candidates(tokens map[string]int) []int {
	if len(tokens) == 0 {
		result := make([]int, len(index.docs))
		for i := range index.docs {
			result[i] = i
		}
		return result
	}

	var current map[int]bool
	for token := range tokens {
		postings := index.tokens[token]
		next := make(map[int]bool, len(postings))
		for _, entry := range postings {
			if current == nil || current[entry.doc] {
				next[entry.doc] = true
			}
		}
		current = next
		if len(current) == 0 {
			return []int{}
		}
	}

	result := make([]int, 0, len(current))
	for docID := range current {
		result = append(result, docID)
	}
	sort.Ints(result)
	return result
}

func countOccurrences(text, query string) int {
	if text == "" || query == "" {
		return 0
	}
	return strings.Count(strings.ToLower(text), strings.ToLower(query))
}

func makeSnippet(content, query string) string {
	if content == "" || query == "" {
		return ""
	}

	runes := []rune(content)
	queryRunes := []rune(query)
	index := indexFold(runes, queryRunes, 0)
	if index < 0 {
		return ""
	}

	start := index - 35
	if start < 0 {
		start = 0
	}
	end := index + len(queryRunes) + 70
	if end > len(runes) {
		end = len(runes)
	}

	snippet := string(runes[start:end])
	snippet = highlightSnippet(snippet, query)
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet
}

func highlightSnippet(text, query string) string {
	if query == "" {
		return text
	}
	runes := []rune(text)
	queryRunes := []rune(query)
	var builder strings.Builder
	position := 0

	for position < len(runes) {
		index := indexFold(runes, queryRunes, position)
		if index < 0 {
			builder.WriteString(string(runes[position:]))
			break
		}
		builder.WriteString(string(runes[position:index]))
		builder.WriteString("[[")
		builder.WriteString(string(runes[index : index+len(queryRunes)]))
		builder.WriteString("]]")
		position = index + len(queryRunes)
	}
	return builder.String()
}

// 忽略大小写比较 rune，避免使用小写文本的字节偏移切分原始字符串。
func indexFold(runes, query []rune, from int) int {
	if from < 0 {
		from = 0
	}
	for start := from; start+len(query) <= len(runes); start++ {
		matched := true
		for offset := range query {
			if unicode.ToLower(runes[start+offset]) != unicode.ToLower(query[offset]) {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}
