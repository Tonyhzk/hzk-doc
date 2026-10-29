package server

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"hzk-doc/internal/content"
)

const (
	maxArchiveEntries   = 10000
	maxArchiveTextBytes = 128 << 20
)

var errArchiveTooLarge = errors.New("素材数量或文本总量超过打包上限")

type archiveEntry struct {
	name        string
	archiveName string
	info        os.FileInfo
	data        []byte
}

func (s *Server) handleDocumentArchive(w http.ResponseWriter, r *http.Request) {
	current := s.current()
	if current == nil {
		writeError(w, http.StatusServiceUnavailable, "文档索引尚未生成")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "缺少文档 ID")
		return
	}
	meta, ok := current.byID[id]
	if !ok {
		writeError(w, http.StatusNotFound, "文档不存在")
		return
	}
	if content.Kind(meta.Path) != "markdown" {
		writeError(w, http.StatusUnsupportedMediaType, "只能打包 Markdown 文档")
		return
	}

	info, err := regularFileInfo(s.root, meta.Path)
	if err != nil {
		s.writeReadError(w, meta.Path, err)
		return
	}
	source, err := s.readArchiveSource(meta.Path, info)
	if err != nil {
		s.writeReadError(w, meta.Path, err)
		return
	}
	folder := strings.TrimSuffix(path.Base(meta.Path), path.Ext(meta.Path))
	entries, err := s.documentArchiveEntries(meta.Path, folder, info, source)
	if err != nil {
		if errors.Is(err, errArchiveTooLarge) || errors.Is(err, errDocumentTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "素材数量或文本总量超过打包上限")
			return
		}
		s.logger.Error("收集文档素材失败", "path", meta.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "素材打包失败")
		return
	}

	filename := folder + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	if err := s.writeDocumentArchive(w, folder, entries); err != nil {
		s.logger.Error("输出文档压缩包失败", "path", meta.Path, "error", err)
	}
}

func (s *Server) readArchiveSource(name string, expected os.FileInfo) ([]byte, error) {
	if expected.Size() > maxDocumentBytes {
		return nil, errDocumentTooLarge
	}
	file, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return nil, os.ErrNotExist
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDocumentBytes {
		return nil, errDocumentTooLarge
	}
	return data, nil
}

func archiveResourceCandidate(documentPath, name string) bool {
	return name != documentPath && content.ValidatePath(name, false) == nil &&
		content.AllowedFile(name) && content.Kind(name) != "markdown"
}

func (s *Server) hasArchiveResources(documentPath string, source []byte) bool {
	for _, name := range content.ResourceReferences(source, documentPath) {
		if !archiveResourceCandidate(documentPath, name) {
			continue
		}
		if _, err := regularFileInfo(s.root, name); err == nil {
			return true
		}
	}
	return false
}

func (s *Server) documentArchiveEntries(documentPath, folder string, info os.FileInfo, source []byte) ([]archiveEntry, error) {
	entries := map[string]archiveEntry{
		documentPath: {name: documentPath, info: info, data: source},
	}
	textBytes := int64(len(source))
	pending := content.ResourceReferences(source, documentPath)
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, exists := entries[name]; exists || !archiveResourceCandidate(documentPath, name) {
			continue
		}
		info, err := regularFileInfo(s.root, name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(entries) >= maxArchiveEntries {
			return nil, errArchiveTooLarge
		}
		entry := archiveEntry{name: name, info: info}
		if content.Kind(name) == "html" || strings.EqualFold(path.Ext(name), ".css") {
			if info.Size() > maxDocumentBytes || info.Size() > maxArchiveTextBytes-textBytes {
				return nil, errArchiveTooLarge
			}
			entry.data, err = s.readArchiveSource(name, info)
			if err != nil {
				return nil, err
			}
			textBytes += int64(len(entry.data))
			if textBytes > maxArchiveTextBytes {
				return nil, errArchiveTooLarge
			}
			pending = append(pending, content.ResourceReferences(entry.data, name)...)
		}
		entries[name] = entry
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	usedNames := make(map[string]bool)
	nextSuffix := make(map[string]int)
	fold := cases.Fold()
	for _, name := range names {
		entry := entries[name]
		if name == documentPath {
			entry.archiveName = path.Base(name)
			if len(entries) > 1 {
				entry.archiveName = path.Join(folder, entry.archiveName)
			}
		} else {
			filename := path.Base(name)
			key := norm.NFC.String(fold.String(filename))
			candidate := filename
			if usedNames[key] {
				extension := path.Ext(filename)
				stem := strings.TrimSuffix(filename, extension)
				suffix := nextSuffix[key]
				if suffix < 2 {
					suffix = 2
				}
				for usedNames[norm.NFC.String(fold.String(candidate))] {
					candidate = fmt.Sprintf("%s-%d%s", stem, suffix, extension)
					suffix++
				}
				nextSuffix[key] = suffix
			}
			usedNames[norm.NFC.String(fold.String(candidate))] = true
			entry.archiveName = path.Join(folder, "assets", candidate)
		}
		entries[name] = entry
	}

	result := make([]archiveEntry, 0, len(names))
	for _, name := range names {
		entry := entries[name]
		switch {
		case name == documentPath && len(entries) > 1:
			entry.data = rewriteArchiveMarkdown(entry.data, name, entries)
		case content.Kind(name) == "html":
			entry.data = []byte(rewriteArchiveHTML(string(entry.data), name, entries))
		case strings.EqualFold(path.Ext(name), ".css"):
			entry.data = []byte(rewriteArchiveCSS(string(entry.data), name, entries))
		}
		result = append(result, entry)
	}
	return result, nil
}

func (s *Server) writeDocumentArchive(output io.Writer, folder string, entries []archiveEntry) error {
	writer := zip.NewWriter(output)
	if len(entries) > 1 {
		for _, name := range []string{folder + "/", path.Join(folder, "assets") + "/"} {
			header := &zip.FileHeader{Name: name, Method: zip.Store}
			header.SetMode(os.ModeDir | 0o755)
			if _, err := writer.CreateHeader(header); err != nil {
				return err
			}
		}
	}
	for _, entry := range entries {
		header, err := zip.FileInfoHeader(entry.info)
		if err != nil {
			return err
		}
		header.Name = entry.archiveName
		header.SetMode(0o644)
		header.Method = zip.Store
		if entry.data != nil {
			header.Method = zip.Deflate
		}
		destination, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.data != nil {
			if _, err := destination.Write(entry.data); err != nil {
				return err
			}
			continue
		}
		if _, err := regularFileInfo(s.root, entry.name); err != nil {
			return err
		}
		file, err := s.root.Open(entry.name)
		if err != nil {
			return err
		}
		opened, statErr := file.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(entry.info, opened) {
			_ = file.Close()
			return fmt.Errorf("素材已变化: %s", entry.name)
		}
		_, copyErr := io.Copy(destination, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return writer.Close()
}

type archiveReplacement struct {
	start int
	end   int
	value string
}

func rewriteArchiveMarkdown(source []byte, name string, entries map[string]archiveEntry) []byte {
	document := markdownRenderer.Parser().Parse(text.NewReader(source))
	var replacements []archiveReplacement
	addDestination := func(position int, destination []byte, marker byte) {
		start := archiveDestinationPosition(source, position, marker, destination)
		if start < 0 {
			return
		}
		original := string(util.URLEscape(destination, true))
		value := archiveRelativeURL(original, name, entries)
		if value != original {
			replacements = append(replacements, archiveReplacement{start, start + len(destination), value})
		}
	}
	addHTML := func(start, end int) {
		if start < 0 || end > len(source) || start >= end {
			return
		}
		original := string(source[start:end])
		value := rewriteArchiveHTML(original, name, entries)
		if value != original {
			replacements = append(replacements, archiveReplacement{start, end, value})
		}
	}
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch current := node.(type) {
		case *ast.Link:
			if current.Reference == nil {
				addDestination(current.Pos(), current.Destination, '(')
			}
		case *ast.Image:
			if current.Reference == nil {
				addDestination(current.Pos(), current.Destination, '(')
			}
		case *ast.LinkReferenceDefinition:
			addDestination(current.Pos(), current.Destination, ':')
		case *ast.RawHTML:
			for i := 0; i < current.Segments.Len(); i++ {
				segment := current.Segments.At(i)
				addHTML(segment.Start, segment.Stop)
			}
		case *ast.HTMLBlock:
			if current.Lines().Len() > 0 {
				start := current.Lines().At(0).Start
				end := current.Lines().At(current.Lines().Len() - 1).Stop
				if current.HasClosure() {
					end = current.ClosureLine.Stop
				}
				addHTML(start, end)
			}
		}
		return ast.WalkContinue, nil
	})
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	var output bytes.Buffer
	position := 0
	for _, replacement := range replacements {
		if replacement.start < position {
			continue
		}
		output.Write(source[position:replacement.start])
		output.WriteString(replacement.value)
		position = replacement.end
	}
	output.Write(source[position:])
	return output.Bytes()
}

func archiveDestinationPosition(source []byte, position int, marker byte, destination []byte) int {
	if position < 0 || position >= len(source) || len(destination) == 0 {
		return -1
	}
	if source[position] == '!' {
		position++
	}
	if position >= len(source) || source[position] != '[' {
		return -1
	}
	depth := 1
	position++
	for position < len(source) && depth > 0 {
		switch source[position] {
		case '\\':
			position += 2
			continue
		case '`':
			start := position
			for position < len(source) && source[position] == '`' {
				position++
			}
			if end := bytes.Index(source[position:], source[start:position]); end >= 0 {
				position += end + position - start
			}
			continue
		case '[':
			depth++
		case ']':
			depth--
		}
		position++
	}
	if depth != 0 || position >= len(source) || source[position] != marker {
		return -1
	}
	position++
	for position < len(source) && (source[position] == ' ' || source[position] == '\t' || source[position] == '\n' || source[position] == '\r') {
		position++
	}
	if position < len(source) && source[position] == '<' {
		position++
	}
	if !bytes.HasPrefix(source[position:], destination) {
		return -1
	}
	return position
}

func archiveRelativeURL(raw, name string, entries map[string]archiveEntry) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Path == "" {
		return raw
	}
	dir := path.Dir(name)
	if dir == "." {
		dir = ""
	}
	target, _, ok := resolveDocReference(raw, dir)
	if !ok {
		return raw
	}
	entry, exists := entries[target]
	if !exists {
		return raw
	}
	relative, err := filepath.Rel(filepath.FromSlash(path.Dir(entries[name].archiveName)), filepath.FromSlash(entry.archiveName))
	if err != nil {
		return raw
	}
	parsed.Path = filepath.ToSlash(relative)
	parsed.RawPath = ""
	parsed.RawPath = strings.ReplaceAll(parsed.EscapedPath(), ",", "%2C")
	return parsed.String()
}

var archiveHTMLAttribute = regexp.MustCompile(`(?i)(?:^|[\s])((?:src|poster|data-src|xlink:href|href|srcset|style)\s*=\s*)(?:"([^"]*)"|'([^']*)'|([^\s"'=<>` + "`" + `]+))`)
var archiveCSSURL = regexp.MustCompile(`(?i)url\(\s*(?:'([^']*)'|"([^"]*)"|([^)'"\s]+))\s*\)`)
var archiveCSSImport = regexp.MustCompile(`(?i)@import\s+(?:"([^"]*)"|'([^']*)')`)

func rewriteArchiveHTML(markup, name string, entries map[string]archiveEntry) string {
	tokenizer := html.NewTokenizer(strings.NewReader(markup))
	var output strings.Builder
	inStyle := false
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			return output.String()
		}
		raw := string(tokenizer.Raw())
		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if strings.EqualFold(token.Data, "style") && tokenType == html.StartTagToken {
				inStyle = true
			}
			raw = rewriteArchiveAttributes(raw, name, entries)
		case html.EndTagToken:
			if strings.EqualFold(tokenizer.Token().Data, "style") {
				inStyle = false
			}
		case html.TextToken:
			if inStyle {
				raw = rewriteArchiveCSS(raw, name, entries)
			}
		}
		output.WriteString(raw)
	}
}

func rewriteArchiveAttributes(tag, name string, entries map[string]archiveEntry) string {
	matches := archiveHTMLAttribute.FindAllStringSubmatchIndex(tag, -1)
	var output strings.Builder
	position := 0
	for _, match := range matches {
		start, end := -1, -1
		for i := 4; i < len(match); i += 2 {
			if match[i] >= 0 {
				start, end = match[i], match[i+1]
				break
			}
		}
		if start < 0 {
			continue
		}
		attribute := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(tag[match[2]:match[3]]), "=")))
		original := tag[start:end]
		value := html.UnescapeString(original)
		switch attribute {
		case "srcset":
			candidates := parseSrcset(value)
			parts := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				target := archiveRelativeURL(candidate.url, name, entries)
				if candidate.descriptors != "" {
					target += " " + candidate.descriptors
				}
				parts = append(parts, target)
			}
			if len(parts) > 0 {
				value = strings.Join(parts, ", ")
			}
		case "style":
			value = rewriteArchiveCSS(value, name, entries)
		default:
			value = archiveRelativeURL(value, name, entries)
		}
		if value == html.UnescapeString(original) {
			continue
		}
		output.WriteString(tag[position:start])
		output.WriteString(html.EscapeString(value))
		position = end
	}
	output.WriteString(tag[position:])
	return output.String()
}

func rewriteArchiveCSS(css, name string, entries map[string]archiveEntry) string {
	rewrite := func(expression *regexp.Regexp, value string) string {
		matches := expression.FindAllStringSubmatchIndex(value, -1)
		var output strings.Builder
		position := 0
		for _, match := range matches {
			for i := 2; i < len(match); i += 2 {
				start, end := match[i], match[i+1]
				if start < 0 {
					continue
				}
				original := value[start:end]
				rewritten := archiveRelativeURL(original, name, entries)
				if rewritten != original {
					output.WriteString(value[position:start])
					output.WriteString(rewritten)
					position = end
				}
				break
			}
		}
		output.WriteString(value[position:])
		return output.String()
	}
	return rewrite(archiveCSSImport, rewrite(archiveCSSURL, css))
}
