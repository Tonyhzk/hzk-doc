package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"hzk-doc/internal/content"
)

const maxDocumentBytes = 64 << 20

var errDocumentTooLarge = errors.New("文档过大")
var errInvalidPath = errors.New("无效路径")

type apiResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

type TocItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

type documentResponse struct {
	Type       string    `json:"type"`
	Path       string    `json:"path"`
	Title      string    `json:"title"`
	Content    string    `json:"content,omitempty"`
	RawContent string    `json:"rawContent,omitempty"`
	URL        string    `json:"url,omitempty"`
	Toc        []TocItem `json:"toc"`
	Size       int64     `json:"size"`
	ModifiedAt string    `json:"modifiedAt,omitempty"`
}

var assetMIMETypes = map[string]string{
	"jpg":   "image/jpeg",
	"jpeg":  "image/jpeg",
	"png":   "image/png",
	"gif":   "image/gif",
	"webp":  "image/webp",
	"svg":   "image/svg+xml",
	"bmp":   "image/bmp",
	"ico":   "image/x-icon",
	"avif":  "image/avif",
	"mp4":   "video/mp4",
	"webm":  "video/webm",
	"mov":   "video/quicktime",
	"m4v":   "video/mp4",
	"ogv":   "video/ogg",
	"mp3":   "audio/mpeg",
	"wav":   "audio/wav",
	"ogg":   "audio/ogg",
	"flac":  "audio/flac",
	"aac":   "audio/aac",
	"m4a":   "audio/mp4",
	"oga":   "audio/ogg",
	"css":   "text/css; charset=utf-8",
	"woff":  "font/woff",
	"woff2": "font/woff2",
	"ttf":   "font/ttf",
	"otf":   "font/otf",
}

var fileMIMETypes = map[string]string{
	"md":       "text/markdown; charset=utf-8",
	"markdown": "text/markdown; charset=utf-8",
	"mark":     "text/markdown; charset=utf-8",
	"html":     "text/html; charset=utf-8",
	"htm":      "text/html; charset=utf-8",
	"pdf":      "application/pdf",
	"docx":     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx":     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx":     "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"txt":      "text/plain; charset=utf-8",
}

func assetMIMEType(extension string) (string, bool) {
	value, ok := assetMIMETypes[strings.ToLower(extension)]
	return value, ok
}

func fileMIMEType(extension string) (string, bool) {
	value, ok := fileMIMETypes[strings.ToLower(extension)]
	return value, ok
}

func (s *Server) handleTree(w http.ResponseWriter, _ *http.Request) {
	current := s.current()
	if current == nil {
		writeError(w, http.StatusServiceUnavailable, "文档索引尚未生成")
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: current.tree})
}

func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	current := s.current()
	if current == nil {
		writeError(w, http.StatusServiceUnavailable, "文档索引尚未生成")
		return
	}

	relative, err := cleanRequestPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文档路径")
		return
	}

	meta, ok := current.docs[relative]
	if !ok {
		writeError(w, http.StatusNotFound, "文档不存在")
		return
	}

	response := documentResponse{
		Path:       relative,
		Title:      meta.Title,
		Toc:        make([]TocItem, 0),
		Size:       meta.Size,
		ModifiedAt: meta.ModTime.UTC().Format(time.RFC3339),
	}

	switch meta.Extension {
	case "md", "markdown", "mark":
		source, err := s.readDocument(relative)
		if err != nil {
			s.writeReadError(w, relative, err)
			return
		}
		rendered, toc, err := renderMarkdown(source, meta.Directory)
		if err != nil {
			s.logger.Error("渲染 Markdown 失败", "path", relative, "error", err)
			writeError(w, http.StatusInternalServerError, "文档渲染失败")
			return
		}
		response.Type = "markdown"
		response.Content = rendered
		response.RawContent = string(source)
		response.Toc = toc
	case "html", "htm":
		response.Type = "html"
		response.URL = "/view/html?path=" + url.QueryEscape(relative)
	case "pdf":
		response.Type = "pdf"
		response.URL = fileURL(relative)
	case "jpg", "jpeg", "png", "gif", "webp", "svg", "bmp", "ico", "avif":
		response.Type = "image"
		response.URL = fileURL(relative)
	case "docx", "xlsx", "pptx":
		response.Type = meta.Extension
		response.URL = fileURL(relative)
	case "txt":
		source, err := s.readDocument(relative)
		if err != nil {
			s.writeReadError(w, relative, err)
			return
		}
		response.Type = "text"
		response.RawContent = string(source)
		response.URL = fileURL(relative)
	case "mp3", "wav", "ogg", "flac", "aac", "m4a", "oga":
		response.Type = "audio"
		response.URL = fileURL(relative)
	case "mp4", "webm", "mov", "m4v", "ogv":
		response.Type = "video"
		response.URL = fileURL(relative)
	default:
		writeError(w, http.StatusUnsupportedMediaType, "不支持的文件类型")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: response})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	current := s.current()
	if current == nil {
		writeError(w, http.StatusServiceUnavailable, "搜索索引尚未生成")
		return
	}

	results, err := current.index.search(r.URL.Query().Get("q"), r.URL.Query().Get("scope"))
	if err != nil {
		switch {
		case errors.Is(err, errQueryTooLong):
			writeError(w, http.StatusBadRequest, "搜索内容不能超过 100 个字符")
		case errors.Is(err, errInvalidScope):
			writeError(w, http.StatusBadRequest, "无效的搜索范围")
		default:
			s.logger.Error("搜索失败", "error", err)
			writeError(w, http.StatusInternalServerError, "搜索失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: results})
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	relative, err := cleanRequestPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件路径")
		return
	}
	if !content.AllowedFile(relative) {
		writeError(w, http.StatusForbidden, "不支持的文件格式")
		return
	}
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(relative), "."))
	mimeType, ok := fileMIMEType(extension)
	if !ok {
		mimeType, ok = assetMIMEType(extension)
	}
	if !ok {
		writeError(w, http.StatusForbidden, "不支持的文件格式")
		return
	}

	headers := map[string]string{
		"Cache-Control": "no-cache",
	}
	switch extension {
	case "svg":
		headers["Content-Security-Policy"] = "sandbox; default-src 'none'; style-src 'unsafe-inline'"
	case "html", "htm":
		headers["Content-Security-Policy"] = "sandbox"
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	s.serveFile(w, r, relative, mimeType, headers, disposition)
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, relative, mimeType string, headers map[string]string, disposition string) {
	if _, err := regularFileInfo(s.root, relative); err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	file, err := s.root.Open(relative)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
	for key, value := range headers {
		w.Header().Set(key, value)
	}
	if disposition != "" {
		value := mime.FormatMediaType(disposition, map[string]string{"filename": info.Name()})
		if value != "" {
			w.Header().Set("Content-Disposition", value)
		}
	}

	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleHTML(w http.ResponseWriter, r *http.Request) {
	relative, err := cleanRequestPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文档路径")
		return
	}
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(relative), "."))
	if !isHTML(extension) {
		writeError(w, http.StatusForbidden, "只能查看 HTML 文件")
		return
	}

	source, err := s.readDocument(relative)
	if err != nil {
		s.writeReadError(w, relative, err)
		return
	}
	docDir := path.Dir(relative)
	if docDir == "." {
		docDir = ""
	}
	rendered, err := rewriteHTMLDocument(source, docDir)
	if err != nil {
		s.logger.Error("处理 HTML 失败", "path", relative, "error", err)
		writeError(w, http.StatusInternalServerError, "HTML 处理失败")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "sandbox")
	_, _ = w.Write(rendered)
}

func (s *Server) readDocument(relative string) ([]byte, error) {
	if _, err := regularFileInfo(s.root, relative); err != nil {
		return nil, err
	}
	file, err := s.root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	if info.Size() > maxDocumentBytes {
		return nil, errDocumentTooLarge
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

func (s *Server) writeReadError(w http.ResponseWriter, relative string, err error) {
	if errors.Is(err, errDocumentTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "文档过大")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "文档不存在")
		return
	}
	s.logger.Error("读取文档失败", "path", relative, "error", err)
	writeError(w, http.StatusInternalServerError, "读取文档失败")
}

func cleanRequestPath(raw string) (string, error) {
	if err := content.ValidatePath(raw, false); err != nil {
		return "", errInvalidPath
	}
	return raw, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiResponse{Success: false, Error: message})
}
