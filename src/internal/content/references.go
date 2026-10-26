package content

import (
	"bytes"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

// TrashDir 保存被删除的文档与资源，位于文档根目录内，不出现在公开目录中。
const TrashDir = ".hzk-doc-trash"

var markdownReferenceRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

var (
	cssURLPattern    = regexp.MustCompile(`(?i)url\(\s*(?:'([^']*)'|"([^"]*)"|([^)'"\s]+))\s*\)`)
	cssImportPattern = regexp.MustCompile(`(?i)@import\s+(?:"([^"]*)"|'([^']*)')`)
)

// ResourceReferences 返回文档引用的站内文件，路径相对文档根目录。
// 统计阅读端会实际请求的位置：Markdown 与 HTML 中的图片、媒体、样式属性、链接目标和 CSS 引用，
// 以及样式表文件中的 url() 与 @import 引用。代码块中的示例内容不是真实引用；
// 外链、data URI、隐藏路径和越界路径不计入。
func ResourceReferences(source []byte, docPath string) []string {
	docDir := path.Dir(docPath)
	if docDir == "." {
		docDir = ""
	}

	seen := make(map[string]bool)
	collect := func(raw string) {
		if resolved, ok := resolveReference(raw, docDir); ok {
			seen[resolved] = true
		}
	}

	if strings.EqualFold(path.Ext(docPath), ".css") {
		stylesheet := string(source)
		collectCSSURLs(stylesheet, collect)
		collectCSSImports(stylesheet, collect)
	} else {
		markup := source
		if Kind(docPath) == "markdown" {
			var rendered bytes.Buffer
			if err := markdownReferenceRenderer.Convert(source, &rendered); err != nil {
				return nil
			}
			markup = rendered.Bytes()
		}

		document, err := html.Parse(bytes.NewReader(markup))
		if err != nil {
			return nil
		}
		collectNodeReferences(document, collect)
	}

	references := make([]string, 0, len(seen))
	for reference := range seen {
		references = append(references, reference)
	}
	sort.Strings(references)
	return references
}

func collectNodeReferences(node *html.Node, collect func(string)) {
	if node.Type == html.ElementNode {
		for _, attribute := range node.Attr {
			switch strings.ToLower(attribute.Key) {
			case "src", "poster", "data-src", "xlink:href", "href":
				collect(attribute.Val)
			case "srcset":
				collectSrcsetReferences(attribute.Val, collect)
			case "style":
				collectCSSURLs(attribute.Val, collect)
			}
		}
		if strings.EqualFold(node.Data, "style") {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.TextNode {
					collectCSSURLs(child.Data, collect)
					collectCSSImports(child.Data, collect)
				}
			}
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectNodeReferences(child, collect)
	}
}

func collectCSSURLs(css string, collect func(string)) {
	for _, match := range cssURLPattern.FindAllStringSubmatch(css, -1) {
		collect(match[1])
		collect(match[2])
		collect(match[3])
	}
}

// collectCSSImports 提取 @import 的字符串形式，紧随其后的媒体条件不参与路径解析。
func collectCSSImports(css string, collect func(string)) {
	for _, match := range cssImportPattern.FindAllStringSubmatch(css, -1) {
		collect(match[1])
		collect(match[2])
	}
}

// collectSrcsetReferences 按 srcset 规则提取候选地址，地址内的逗号（如 data URI）不作为分隔符。
func collectSrcsetReferences(value string, collect func(string)) {
	for index := 0; index < len(value); {
		for index < len(value) && (isASCIIWhitespace(value[index]) || value[index] == ',') {
			index++
		}
		if index >= len(value) {
			return
		}

		start := index
		for index < len(value) && !isASCIIWhitespace(value[index]) {
			index++
		}
		candidate := value[start:index]
		if strings.HasSuffix(candidate, ",") {
			collect(strings.TrimRight(candidate, ","))
			continue
		}
		collect(candidate)

		depth := 0
		for index < len(value) && !(value[index] == ',' && depth == 0) {
			switch value[index] {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			}
			index++
		}
		index++
	}
}

func isASCIIWhitespace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	default:
		return false
	}
}

// resolveReference 把文档中的引用解析为文档根目录下的相对路径，规则与阅读端一致。
func resolveReference(raw, docDir string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\\\x00") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return "", false
	}
	reference := parsed.Path
	if reference == "" {
		return "", false
	}
	switch {
	case strings.HasPrefix(reference, "/"):
		reference = strings.TrimPrefix(reference, "/")
	case reference == "assets" || strings.HasPrefix(reference, "assets/"):
	default:
		reference = path.Join(docDir, reference)
	}
	reference = path.Clean(reference)
	if reference == "." || reference == ".." || strings.HasPrefix(reference, "../") {
		return "", false
	}
	for _, segment := range strings.Split(reference, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return "", false
		}
	}
	return reference, true
}
