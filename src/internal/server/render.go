package server

import (
	"bytes"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

var markdownSanitizer = newMarkdownSanitizer()

func newMarkdownSanitizer() *bluemonday.Policy {
	policy := bluemonday.UGCPolicy()
	policy.AllowElements("video", "audio", "source", "details", "summary", "kbd", "mark", "figure", "figcaption")
	policy.AllowAttrs("src", "controls", "poster", "preload", "loop", "muted", "playsinline", "width", "height").OnElements("video")
	policy.AllowAttrs("src", "controls", "preload", "loop").OnElements("audio")
	policy.AllowAttrs("src", "type").OnElements("source")
	policy.AllowAttrs("class").Globally()
	policy.AllowAttrs("id").Globally()
	policy.AllowAttrs("colspan", "rowspan").OnElements("td", "th")
	policy.AllowAttrs("loading").OnElements("img")
	policy.AllowDataURIImages()
	return policy
}

var cssURLPattern = regexp.MustCompile(`url\(\s*(?:'([^']*)'|"([^"]*)"|([^)'"\s]+))\s*\)`)

type headingIDs struct {
	values map[string]bool
}

func newHeadingIDs() *headingIDs {
	return &headingIDs{values: make(map[string]bool)}
}

func (ids *headingIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	var builder strings.Builder
	for _, character := range strings.ToLower(string(value)) {
		switch {
		case unicode.IsLetter(character), unicode.IsDigit(character), character == '_', character == '-':
			builder.WriteRune(character)
		case unicode.IsSpace(character):
			builder.WriteByte('-')
		}
	}

	base := builder.String()
	if base == "" {
		if kind == ast.KindHeading {
			base = "heading"
		} else {
			base = "id"
		}
	}
	candidate := base
	for index := 1; ids.values[candidate]; index++ {
		candidate = fmt.Sprintf("%s-%d", base, index)
	}
	ids.values[candidate] = true
	return []byte(candidate)
}

func (ids *headingIDs) Put(value []byte) {
	ids.values[string(value)] = true
}

func renderMarkdown(source []byte, docDir string) (string, []TocItem, error) {
	var rendered bytes.Buffer
	context := parser.NewContext(parser.WithIDs(newHeadingIDs()))
	if err := markdownRenderer.Convert(source, &rendered, parser.WithContext(context)); err != nil {
		return "", nil, err
	}
	sanitized := markdownSanitizer.Sanitize(rendered.String())
	output, toc := rewriteMarkdownFragment(sanitized, docDir)
	return output, toc, nil
}

func rewriteMarkdownFragment(fragment, docDir string) (string, []TocItem) {
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return fragment, []TocItem{}
	}

	for _, node := range nodes {
		rewriteNodeResources(node, docDir)
	}
	toc := collectTOC(nodes)

	var output bytes.Buffer
	for _, node := range nodes {
		if err := html.Render(&output, node); err != nil {
			return fragment, toc
		}
	}
	return output.String(), toc
}

func collectTOC(nodes []*html.Node) []TocItem {
	toc := make([]TocItem, 0)
	used := make(map[string]bool)
	headingIndex := 0

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if level := headingLevel(node.Data); level > 0 {
				headingIndex++
				text := strings.Join(strings.Fields(nodeText(node)), " ")
				if text != "" {
					id := nodeAttribute(node, "id")
					if id == "" {
						id = fmt.Sprintf("section-%d", headingIndex)
					}
					if used[id] {
						for suffix := 2; ; suffix++ {
							candidate := fmt.Sprintf("%s-%d", id, suffix)
							if !used[candidate] {
								id = candidate
								break
							}
						}
					}
					setNodeAttribute(node, "id", id)
					used[id] = true
					toc = append(toc, TocItem{ID: id, Text: text, Level: level})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	return toc
}

func headingLevel(tag string) int {
	switch tag {
	case "h1":
		return 1
	case "h2":
		return 2
	case "h3":
		return 3
	default:
		return 0
	}
}

func nodeText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func nodeAttribute(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return attribute.Val
		}
	}
	return ""
}

func setNodeAttribute(node *html.Node, key, value string) {
	for index := range node.Attr {
		if strings.EqualFold(node.Attr[index].Key, key) {
			node.Attr[index].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: value})
}

func rewriteHTMLDocument(source []byte, docDir string) ([]byte, error) {
	document, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	sanitizeHTMLNode(document)
	rewriteNodeResources(document, docDir)

	var output bytes.Buffer
	if err := html.Render(&output, document); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

var blockedHTMLElements = map[string]bool{
	"script": true, "form": true, "input": true, "button": true,
	"select": true, "textarea": true, "option": true, "optgroup": true,
	"iframe": true, "frame": true, "frameset": true, "object": true,
	"embed": true, "applet": true, "base": true, "meta": true,
}

func sanitizeHTMLNode(node *html.Node) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.ElementNode {
			tag := strings.ToLower(child.Data)
			if blockedHTMLElements[tag] || (tag == "link" && !isStylesheetLink(child)) {
				node.RemoveChild(child)
				child = next
				continue
			}
			sanitizeHTMLAttributes(child)
		}
		sanitizeHTMLNode(child)
		child = next
	}
}

func sanitizeHTMLAttributes(node *html.Node) {
	filtered := node.Attr[:0]
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		if strings.HasPrefix(key, "on") ||
			key == "target" || key == "formaction" || key == "srcdoc" ||
			key == "ping" || key == "http-equiv" {
			continue
		}
		if key == "href" || key == "src" || key == "poster" || key == "data-src" || key == "xlink:href" {
			if dangerousURL(attribute.Val) {
				continue
			}
		}
		filtered = append(filtered, attribute)
	}
	node.Attr = filtered
}

func isStylesheetLink(node *html.Node) bool {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, "rel") {
			for _, token := range strings.Fields(strings.ToLower(attribute.Val)) {
				if token == "stylesheet" {
					return true
				}
			}
		}
	}
	return false
}

func rewriteNodeResources(node *html.Node, docDir string) {
	if node.Type == html.ElementNode {
		attributes := make([]html.Attribute, 0, len(node.Attr)+2)
		openInNewTab := false
		for _, attribute := range node.Attr {
			switch strings.ToLower(attribute.Key) {
			case "target":
				continue
			case "style":
				attribute.Val = rewriteCSSURLs(attribute.Val, docDir)
			case "href":
				if node.Data == "a" {
					var blank bool
					attribute.Val, blank = rewriteAnchorValue(attribute.Val, docDir)
					openInNewTab = openInNewTab || blank
				} else {
					attribute.Val = rewriteResourceValue(attribute.Val, docDir)
				}
			case "src", "poster", "data-src", "xlink:href":
				attribute.Val = rewriteResourceValue(attribute.Val, docDir)
			case "srcset":
				attribute.Val = rewriteSrcset(attribute.Val, docDir)
			}
			attributes = append(attributes, attribute)
		}
		if openInNewTab {
			attributes = setTargetBlank(attributes)
		}
		node.Attr = attributes

		if node.Data == "style" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.TextNode {
					child.Data = rewriteCSSURLs(child.Data, docDir)
				}
			}
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		rewriteNodeResources(child, docDir)
	}
}

func rewriteAnchorValue(raw, docDir string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw, false
	}
	if dangerousURL(raw) || strings.HasPrefix(strings.ToLower(raw), "data:") {
		return "#", false
	}
	if strings.HasPrefix(raw, "#") {
		return raw, false
	}
	if isHTTPURL(raw) {
		return raw, true
	}
	if hasScheme(raw) {
		return raw, false
	}

	relative, fragment, ok := resolveDocReference(raw, docDir)
	if !ok {
		return raw, false
	}
	target, ok := viewerURL(relative)
	if !ok {
		return raw, false
	}
	return target + fragment, true
}

func rewriteResourceValue(raw, docDir string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if dangerousURL(raw) {
		return "#"
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:") || strings.HasPrefix(raw, "#") {
		return raw
	}
	if isHTTPURL(raw) || strings.HasPrefix(raw, "//") || hasScheme(raw) {
		return raw
	}

	relative, fragment, ok := resolveDocReference(raw, docDir)
	if !ok {
		return raw
	}
	target, ok := resourceURL(relative)
	if !ok {
		return raw
	}
	return target + fragment
}

func rewriteCSSURLs(css, docDir string) string {
	return cssURLPattern.ReplaceAllStringFunc(css, func(match string) string {
		parts := cssURLPattern.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		raw := parts[1]
		if raw == "" {
			raw = parts[2]
		}
		if raw == "" {
			raw = parts[3]
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return match
		}
		if dangerousURL(raw) {
			return `url("")`
		}
		if strings.HasPrefix(strings.ToLower(raw), "data:") {
			return match
		}
		if isHTTPURL(raw) || strings.HasPrefix(raw, "//") || hasScheme(raw) {
			return match
		}
		relative, fragment, ok := resolveDocReference(raw, docDir)
		if !ok {
			return match
		}
		target, ok := resourceURL(relative)
		if !ok {
			return match
		}
		return `url("` + target + fragment + `")`
	})
}

func rewriteSrcset(value, docDir string) string {
	candidates := parseSrcset(value)
	if len(candidates) == 0 {
		return value
	}

	parts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		target := rewriteResourceValue(candidate.url, docDir)
		if candidate.descriptors != "" {
			target += " " + candidate.descriptors
		}
		parts = append(parts, target)
	}
	return strings.Join(parts, ", ")
}

type srcsetCandidate struct {
	url         string
	descriptors string
}

// 按 srcset 规则收集 URL 与描述符，URL 内的逗号（如 data URI）不拆分。
func parseSrcset(value string) []srcsetCandidate {
	candidates := make([]srcsetCandidate, 0, 4)
	index := 0
	for index < len(value) {
		for index < len(value) && (isASCIIWhitespace(value[index]) || value[index] == ',') {
			index++
		}
		if index >= len(value) {
			break
		}

		urlStart := index
		for index < len(value) && !isASCIIWhitespace(value[index]) {
			index++
		}
		rawURL := value[urlStart:index]
		if strings.HasSuffix(rawURL, ",") {
			rawURL = strings.TrimRight(rawURL, ",")
			if rawURL == "" {
				continue
			}
			candidates = append(candidates, srcsetCandidate{url: rawURL})
			continue
		}

		descriptorStart := index
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
		descriptors := strings.TrimSpace(value[descriptorStart:index])
		if index < len(value) {
			index++
		}
		candidates = append(candidates, srcsetCandidate{url: rawURL, descriptors: descriptors})
	}
	return candidates
}

func isASCIIWhitespace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	default:
		return false
	}
}

func resolveDocReference(raw, docDir string) (string, string, bool) {
	if strings.ContainsRune(raw, '\\') || strings.ContainsRune(raw, '\x00') {
		return "", "", false
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return "", "", false
	}

	reference := parsed.Path
	if reference == "" {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(reference, "/"):
		reference = strings.TrimPrefix(reference, "/")
	case reference == "assets" || strings.HasPrefix(reference, "assets/"):
		// assets 目录由整个文档根共享。
	default:
		reference = path.Join(docDir, reference)
	}
	reference = path.Clean(reference)
	if reference == "." || reference == ".." || strings.HasPrefix(reference, "../") {
		return "", "", false
	}
	for _, segment := range strings.Split(reference, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return "", "", false
		}
	}

	fragment := ""
	if parsed.Fragment != "" {
		fragment = "#" + parsed.Fragment
	}
	return reference, fragment, true
}

func viewerURL(relative string) (string, bool) {
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(relative), "."))
	if extension == "md" || extension == "markdown" || extension == "mark" {
		return fileURL(relative), true
	}
	if isHTML(extension) {
		return "/view/html?path=" + url.QueryEscape(relative), true
	}
	if _, ok := fileMIMEType(extension); ok {
		return fileURL(relative), true
	}
	if _, ok := assetMIMEType(extension); ok {
		return fileURL(relative), true
	}
	return "", false
}

func resourceURL(relative string) (string, bool) {
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(relative), "."))
	if isHTML(extension) {
		return "/view/html?path=" + url.QueryEscape(relative), true
	}
	if _, ok := assetMIMEType(extension); ok {
		return fileURL(relative), true
	}
	if _, ok := fileMIMEType(extension); ok {
		return fileURL(relative), true
	}
	return "", false
}

func fileURL(relative string) string {
	return "/file?path=" + url.QueryEscape(relative)
}

func isHTML(extension string) bool {
	return extension == "html" || extension == "htm"
}

func hasScheme(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme != ""
}

func isHTTPURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "//")
}

func dangerousURL(raw string) bool {
	// 浏览器识别 scheme 前会忽略 ASCII 控制字符，判定前先做同样规范化。
	normalized := make([]byte, 0, len(raw))
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		if character <= 0x1f || character == 0x7f {
			continue
		}
		normalized = append(normalized, character)
	}

	lower := strings.ToLower(strings.TrimSpace(string(normalized)))
	return strings.HasPrefix(lower, "javascript:") ||
		strings.HasPrefix(lower, "vbscript:") ||
		strings.HasPrefix(lower, "file:") ||
		strings.HasPrefix(lower, "data:text/") ||
		strings.HasPrefix(lower, "data:application/")
}

func setTargetBlank(attributes []html.Attribute) []html.Attribute {
	for index := range attributes {
		if strings.EqualFold(attributes[index].Key, "rel") {
			attributes[index].Val = "noopener noreferrer"
			return append(attributes, html.Attribute{Key: "target", Val: "_blank"})
		}
	}
	return append(attributes,
		html.Attribute{Key: "target", Val: "_blank"},
		html.Attribute{Key: "rel", Val: "noopener noreferrer"},
	)
}
