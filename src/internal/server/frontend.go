package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

type siteConfig struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	Count           int    `json:"count"`
	AdminEnabled    bool   `json:"adminEnabled"`
	Theme           string `json:"theme"`
	DownloadEnabled bool   `json:"downloadEnabled"`
}

func (s *Server) handleSite(w http.ResponseWriter, _ *http.Request) {
	name, err := s.catalog.SiteName()
	if err != nil {
		s.logger.Error("读取站点名称失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取站点信息失败")
		return
	}
	if name == "" {
		name = s.config.SiteName
	}
	theme, err := s.catalog.SiteTheme()
	if err != nil {
		s.logger.Error("读取主题失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取站点信息失败")
		return
	}
	downloadEnabled, err := s.catalog.DownloadEnabled()
	if err != nil {
		s.logger.Error("读取下载开关失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取站点信息失败")
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: s.siteInfo(name, theme, downloadEnabled)})
}

func (s *Server) siteInfo(name, theme string, downloadEnabled bool) siteConfig {
	count := 0
	if current := s.current(); current != nil {
		count = len(current.docs)
	}
	return siteConfig{
		Name:            name,
		Version:         s.config.Version,
		Count:           count,
		AdminEnabled:    s.config.AdminEnabled,
		Theme:           theme,
		DownloadEnabled: downloadEnabled,
	}
}

func (s *Server) frontendHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "接口不存在")
			return
		}

		cleaned := path.Clean("/" + r.URL.Path)
		if cleaned == "/" || cleaned == "/admin" {
			s.serveIndex(w, r)
			return
		}

		requestPath := strings.TrimPrefix(cleaned, "/")
		if strings.ContainsAny(requestPath, "\\\x00") || hasHiddenSegment(requestPath) {
			http.Error(w, "页面不存在", http.StatusNotFound)
			return
		}

		if info, err := fs.Stat(s.assets, requestPath); err == nil && info.Mode().IsRegular() {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if requestPath == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		http.Error(w, "页面不存在", http.StatusNotFound)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		http.Error(w, "页面不存在", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(data))
}

func hasHiddenSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}
