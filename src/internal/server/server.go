package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	DocsDir         string
	SiteName        string
	Version         string
	AdminEnabled    bool
	RefreshInterval time.Duration
}

type newOptions struct {
	logger       *slog.Logger
	pdfExtractor func(path string) (string, error)
}

var _ http.Handler = (*Server)(nil)

type Server struct {
	config     Config
	root       *os.Root
	assets     fs.FS
	handler    http.Handler
	logger     *slog.Logger
	pdfExtract func(path string) (string, error)

	state       atomic.Pointer[state]
	rebuildMu   sync.Mutex
	closed      atomic.Bool
	stop        chan struct{}
	refreshDone chan struct{}
	closeDone   chan struct{}
	closeErr    error
}

type state struct {
	tree        []TreeEntry
	docs        map[string]DocMeta
	index       *searchIndex
	fingerprint string
}

func New(cfg Config, assets fs.FS) (*Server, error) {
	return newServer(cfg, assets, newOptions{})
}

func newServer(cfg Config, assets fs.FS, options newOptions) (*Server, error) {
	if strings.TrimSpace(cfg.DocsDir) == "" {
		return nil, errors.New("必须指定文档目录")
	}

	rootPath, err := filepath.Abs(cfg.DocsDir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}

	logger := options.logger
	if logger == nil {
		logger = slog.Default()
	}

	extractor := options.pdfExtractor
	if extractor == nil {
		extractor = defaultPDFTextExtractor(logger, root)
	}

	if strings.TrimSpace(cfg.SiteName) == "" {
		cfg.SiteName = "文档"
	}

	s := &Server{
		config:     cfg,
		root:       root,
		assets:     assets,
		logger:     logger,
		pdfExtract: extractor,
		stop:       make(chan struct{}),
		closeDone:  make(chan struct{}),
	}
	if err := s.rebuild(true); err != nil {
		_ = root.Close()
		return nil, err
	}
	s.handler = s.buildHandler()
	if cfg.RefreshInterval > 0 {
		s.refreshDone = make(chan struct{})
		go s.refreshLoop(cfg.RefreshInterval)
	}
	return s, nil
}

func defaultPDFTextExtractor(logger *slog.Logger, root *os.Root) func(string) (string, error) {
	binary, _ := exec.LookPath("pdftotext")
	if binary == "" {
		logger.Info("未找到 pdftotext，使用纯 Go PDF 提取")
	}
	return func(name string) (string, error) {
		if _, err := regularFileInfo(root, name); err != nil {
			return "", err
		}
		file, err := root.Open(name)
		if err != nil {
			return "", err
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", os.ErrNotExist
		}
		if info.Size() > maxDocumentBytes {
			return "", errDocumentTooLarge
		}

		text, err := extractPDFPureGo(file, info.Size())
		if err == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
		if binary != "" {
			fallback, fallbackErr := extractPDFText(binary, file)
			if fallbackErr == nil && strings.TrimSpace(fallback) != "" {
				return fallback, nil
			}
		}
		if err != nil {
			return "", err
		}
		return text, nil
	}
}

// 逐级检查以拒绝符号链接，防止通过链接访问隐藏元数据。
func regularFileInfo(root *os.Root, name string) (os.FileInfo, error) {
	segments := strings.Split(name, "/")
	for index := 1; index <= len(segments); index++ {
		prefix := strings.Join(segments[:index], "/")
		info, err := root.Lstat(prefix)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, os.ErrNotExist
		}
		if index == len(segments) {
			if !info.Mode().IsRegular() {
				return nil, os.ErrNotExist
			}
			return info, nil
		}
	}
	return nil, os.ErrNotExist
}

func (s *Server) Refresh() error {
	return s.rebuild(true)
}

func (s *Server) refreshIfChanged() error {
	return s.rebuild(false)
}

func (s *Server) rebuild(force bool) error {
	if s.closed.Load() {
		return errors.New("服务已关闭")
	}

	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()

	if s.closed.Load() {
		return errors.New("服务已关闭")
	}

	tree, docs, err := s.scanTree()
	if err != nil {
		return err
	}
	fingerprint := docsFingerprint(docs) + "\x00" + treeFingerprint(tree)
	if !force {
		if current := s.current(); current != nil && current.fingerprint == fingerprint {
			return nil
		}
	}

	index := buildSearchIndex(s, docs)
	s.state.Store(&state{
		tree:        tree,
		docs:        docs,
		index:       index,
		fingerprint: fingerprint,
	})
	return nil
}

func docsFingerprint(docs map[string]DocMeta) string {
	paths := make([]string, 0, len(docs))
	for documentPath := range docs {
		paths = append(paths, documentPath)
	}
	sort.Strings(paths)

	hash := sha256.New()
	for _, documentPath := range paths {
		meta := docs[documentPath]
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%d\n", meta.Path, meta.Size, meta.ModTime.UnixNano())
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func treeFingerprint(tree []TreeEntry) string {
	data, err := json.Marshal(tree)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Server) PreviewMarkdown(documentPath, source string) (string, error) {
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(documentPath), "."))
	switch extension {
	case "md", "markdown", "mark":
		documentDir := path.Dir(documentPath)
		if documentDir == "." {
			documentDir = ""
		}
		rendered, _, err := renderMarkdown([]byte(source), documentDir)
		return rendered, err
	default:
		return source, nil
	}
}

func (s *Server) refreshLoop(interval time.Duration) {
	defer close(s.refreshDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.refreshIfChanged(); err != nil {
				if s.closed.Load() {
					return
				}
				s.logger.Warn("刷新文档失败", "error", err)
			}
		case <-s.stop:
			return
		}
	}
}

func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	if s.assets != nil {
		mux.Handle("/", s.frontendHandler())
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusNotFound, "接口不存在")
				return
			}
			http.Error(w, "页面不存在", http.StatusNotFound)
		})
	}
	return mux
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/site", s.handleSite)
	mux.HandleFunc("GET /api/tree", s.handleTree)
	mux.HandleFunc("GET /api/doc", s.handleDoc)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /file", s.handleFile)
	mux.HandleFunc("HEAD /file", s.handleFile)
	mux.HandleFunc("GET /view/html", s.handleHTML)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.handler == nil {
		http.Error(w, "服务尚未初始化", http.StatusServiceUnavailable)
		return
	}
	s.handler.ServeHTTP(w, r)
}

func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		<-s.closeDone
		return s.closeErr
	}
	close(s.stop)
	if s.refreshDone != nil {
		<-s.refreshDone
	}

	s.rebuildMu.Lock()
	err := s.root.Close()
	s.rebuildMu.Unlock()
	s.closeErr = err
	close(s.closeDone)
	return err
}

func (s *Server) current() *state {
	return s.state.Load()
}
