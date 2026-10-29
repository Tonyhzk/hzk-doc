package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"hzk-doc/internal/catalog"
	"hzk-doc/internal/content"
)

const (
	cookieName = "hzk_doc_admin"

	minPasswordRunes     = 12
	sessionTTL           = 12 * time.Hour
	maxSessions          = 64
	maxLoginBody         = 4 << 10
	maxDocumentBody      = 64 << 20
	maxDocumentBytes     = 64 << 20
	maxUploadBytes       = 64 << 20
	maxMultipartOverhead = 1 << 20
	maxOrderBytes        = 1 << 20

	loginMaxFailures = 5
	loginWindow      = time.Minute
	loginBlock       = 5 * time.Minute
	maxLoginClients  = 4096
)

var (
	errSymlink        = errors.New("不允许符号链接")
	errNotRegular     = errors.New("不是普通文件")
	errNotDir         = errors.New("不是目录")
	errInvalidName    = errors.New("名称无效")
	errTooLarge       = errors.New("内容过大")
	errMalformedOrder = errors.New("排序元数据无效")
)

// Config 控制管理接口。
type Config struct {
	Catalog *catalog.Store
	// DocsDir 是文档根目录，必填。
	DocsDir string
	// Password 非空时启用单管理员账号，至少 12 个字符。
	Password string
	// OnChange 在成功写入后调用，可为空。
	OnChange func() error
	// Preview 渲染可编辑文档，可为空。
	Preview func(path, source string) (string, error)
}

// Handler 在 /api/admin/ 下提供管理接口。
type Handler struct {
	catalog      *catalog.Store
	ownsCatalog  bool
	root         *os.Root
	enabled      bool
	passwordHash [sha256.Size]byte
	onChange     func() error
	preview      func(path, source string) (string, error)

	now func() time.Time

	files      sync.RWMutex
	sessionsMu sync.Mutex
	sessions   map[string]session
	limitsMu   sync.Mutex
	limits     map[string]*loginLimit

	closed atomic.Bool
}

type session struct {
	csrf    string
	expires time.Time
}

type loginLimit struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

// New 打开文档根目录并返回管理接口处理器。
func New(cfg Config) (*Handler, error) {
	if strings.TrimSpace(cfg.DocsDir) == "" {
		return nil, errors.New("文档目录不能为空")
	}
	if cfg.Password != "" && utf8.RuneCountInString(cfg.Password) < minPasswordRunes {
		return nil, errors.New("管理员密码至少需要 12 个字符")
	}

	rootPath, err := filepath.Abs(cfg.DocsDir)
	if err != nil {
		return nil, errors.New("无法解析文档目录")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, errors.New("无法打开文档目录")
	}

	h := &Handler{
		root:     root,
		enabled:  cfg.Password != "",
		onChange: cfg.OnChange,
		preview:  cfg.Preview,
		now:      time.Now,
		sessions: make(map[string]session),
		limits:   make(map[string]*loginLimit),
	}
	h.catalog = cfg.Catalog
	if h.catalog == nil {
		h.catalog, err = catalog.Open(rootPath)
		if err != nil {
			root.Close()
			return nil, err
		}
		h.ownsCatalog = true
	}
	if h.enabled {
		h.passwordHash = sha256.Sum256([]byte(cfg.Password))
	}
	return h, nil
}

// Close 释放文档根目录，可重复调用。
func (h *Handler) Close() error {
	if !h.closed.CompareAndSwap(false, true) {
		return nil
	}
	h.files.Lock()
	defer h.files.Unlock()
	err := h.root.Close()
	if h.ownsCatalog {
		err = errors.Join(err, h.catalog.Close())
	}
	return err
}

// ServeHTTP 接收完整的 /api/admin/ 路径。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if h.closed.Load() {
		writeError(w, newAPIError(http.StatusServiceUnavailable, "服务已关闭"))
		return
	}

	data, apiErr := h.dispatch(w, r)
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	status := http.StatusOK
	if result, ok := data.(createdResult); ok {
		status = http.StatusCreated
		data = result.data
	}
	writeSuccess(w, status, data)
}

func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	switch r.URL.Path {
	case "/api/admin/session":
		if e := requireMethod(r, http.MethodGet); e != nil {
			return nil, e
		}
		return h.handleSession(r)
	case "/api/admin/login":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleLogin(w, r)
	case "/api/admin/logout":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleLogout(w, r)
	case "/api/admin/entries":
		if e := requireMethod(r, http.MethodGet); e != nil {
			return nil, e
		}
		return h.handleEntries(r)
	case "/api/admin/source":
		if e := requireMethod(r, http.MethodGet); e != nil {
			return nil, e
		}
		return h.handleSource(r)
	case "/api/admin/document":
		switch r.Method {
		case http.MethodPost:
			return h.handleCreateDocument(w, r)
		case http.MethodPut:
			return h.handleEditDocument(w, r)
		default:
			return nil, methodNotAllowed(http.MethodPost + ", " + http.MethodPut)
		}
	case "/api/admin/rename":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleRename(w, r)
	case "/api/admin/folder":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleCreateFolder(w, r)
	case "/api/admin/upload":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleUpload(w, r)
	case "/api/admin/theme":
		if e := requireMethod(r, http.MethodPut); e != nil {
			return nil, e
		}
		return h.handleTheme(w, r)
	case "/api/admin/order":
		if e := requireMethod(r, http.MethodPut); e != nil {
			return nil, e
		}
		return h.handleOrder(w, r)
	case "/api/admin/preview":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handlePreview(w, r)
	case "/api/admin/delete":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleDelete(w, r)
	case "/api/admin/trash":
		if e := requireMethod(r, http.MethodGet); e != nil {
			return nil, e
		}
		return h.handleTrashList(r)
	case "/api/admin/trash/restore":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleTrashRestore(w, r)
	case "/api/admin/trash/purge":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleTrashPurge(w, r)
	case "/api/admin/trash/empty":
		if e := requireMethod(r, http.MethodPost); e != nil {
			return nil, e
		}
		return h.handleTrashEmpty(w, r)
	default:
		return nil, newAPIError(http.StatusNotFound, "接口不存在")
	}
}

type apiError struct {
	status  int
	message string
	allow   string
}

func newAPIError(status int, message string) *apiError {
	return &apiError{status: status, message: message}
}

func methodNotAllowed(allow string) *apiError {
	e := newAPIError(http.StatusMethodNotAllowed, "方法不允许")
	e.allow = allow
	return e
}

func requireMethod(r *http.Request, method string) *apiError {
	if r.Method != method {
		return methodNotAllowed(method)
	}
	return nil
}

func (h *Handler) mutate(fn func() *apiError) *apiError {
	h.files.Lock()
	h.catalog.Lock()
	var apiErr *apiError
	if err := h.catalog.Recover(); err != nil {
		apiErr = apiErrorFor(err)
	} else if err := h.catalog.Sync(); err != nil {
		apiErr = apiErrorFor(err)
	} else {
		apiErr = fn()
		if apiErr == nil {
			if err := h.catalog.Sync(); err != nil {
				apiErr = apiErrorFor(err)
			}
		}
	}
	h.catalog.Unlock()
	h.files.Unlock()
	if apiErr != nil {
		return apiErr
	}
	if h.onChange != nil {
		if err := h.onChange(); err != nil {
			return newAPIError(http.StatusInternalServerError, "文件已保存，但刷新失败")
		}
	}
	return nil
}

func (h *Handler) decodeJSON(w http.ResponseWriter, r *http.Request, max int64, dst any) *apiError {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return newAPIError(http.StatusUnsupportedMediaType, "请求必须是 JSON")
	}

	r.Body = http.MaxBytesReader(w, r.Body, max)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return newAPIError(http.StatusRequestEntityTooLarge, "请求内容过大")
		}
		return newAPIError(http.StatusBadRequest, "请求格式无效")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return newAPIError(http.StatusBadRequest, "请求格式无效")
	}
	return nil
}

func (h *Handler) requireSession(r *http.Request) (*session, *apiError) {
	if !h.enabled {
		return nil, newAPIError(http.StatusForbidden, "管理功能未启用")
	}
	sess, _ := h.sessionFromRequest(r)
	if sess == nil {
		return nil, newAPIError(http.StatusUnauthorized, "未登录")
	}
	return sess, nil
}

func (h *Handler) requireWriteAccess(r *http.Request) (*session, *apiError) {
	sess, apiErr := h.requireSession(r)
	if apiErr != nil {
		return nil, apiErr
	}
	if !sameOrigin(r) {
		return nil, newAPIError(http.StatusForbidden, "请求来源无效")
	}
	if !csrfMatches(sess, r.Header.Get("X-CSRF-Token")) {
		return nil, newAPIError(http.StatusForbidden, "CSRF 校验失败")
	}
	return sess, nil
}

func sameOrigin(r *http.Request) bool {
	scheme, ok := parseOrigin(r)
	if !ok {
		return false
	}
	if r.TLS != nil {
		return scheme == "https"
	}
	return true
}

func parseOrigin(r *http.Request) (string, bool) {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" || strings.ContainsAny(origin, "?#") {
		return "", false
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" {
		return "", false
	}
	if u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || u.RawFragment != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return "", false
	}
	return scheme, true
}

func secureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	scheme, ok := parseOrigin(r)
	return ok && scheme == "https"
}

func (h *Handler) lstatPath(name string) (fs.FileInfo, error) {
	if name == "" {
		return h.root.Lstat(".")
	}

	parts := strings.Split(name, "/")
	var last fs.FileInfo
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		info, err := h.root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, errSymlink
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, errNotDir
		}
		last = info
	}
	return last, nil
}

func (h *Handler) openDir(name string) (*os.File, error) {
	info, err := h.lstatPath(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errNotDir
	}
	target := name
	if target == "" {
		target = "."
	}
	file, err := h.root.Open(target)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (h *Handler) ensureParentDir(name string) error {
	parent := path.Dir(name)
	if parent == "." {
		parent = ""
	}
	if parent == "" {
		return nil
	}
	info, err := h.lstatPath(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errNotDir
	}
	return nil
}

func (h *Handler) readRegularFile(name string) ([]byte, fs.FileInfo, error) {
	info, err := h.lstatPath(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errNotRegular
	}

	file, err := h.root.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, nil, errNotRegular
	}

	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > maxDocumentBytes {
		return nil, nil, errTooLarge
	}
	return data, opened, nil
}

func (h *Handler) writeNewFile(name string, data []byte) error {
	file, err := h.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = h.root.Remove(name)
		}
	}()

	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func (h *Handler) writeFileAtomic(name string, data []byte, perm fs.FileMode) error {
	tmp, err := h.tempName(path.Dir(name), ".hzk-tmp-")
	if err != nil {
		return err
	}
	file, err := h.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = h.root.Remove(tmp)
		}
	}()

	if err := file.Chmod(perm); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := h.root.Rename(tmp, name); err != nil {
		return err
	}
	remove = false
	return nil
}

func (h *Handler) tempName(dir, prefix string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", errors.New("无法生成临时文件名")
	}
	name := prefix + token
	if dir != "." {
		name = path.Join(dir, name)
	}
	return name, nil
}

func apiErrorFor(err error) *apiError {
	switch {
	case errors.Is(err, errSymlink), errors.Is(err, errNotRegular),
		errors.Is(err, errNotDir), errors.Is(err, errInvalidName):
		return newAPIError(http.StatusBadRequest, "路径无效")
	case errors.Is(err, errTooLarge):
		return newAPIError(http.StatusRequestEntityTooLarge, "内容过大")
	case errors.Is(err, fs.ErrNotExist):
		return newAPIError(http.StatusNotFound, "文件或目录不存在")
	case errors.Is(err, fs.ErrExist), errors.Is(err, catalog.ErrConflict):
		return newAPIError(http.StatusConflict, "目标已存在")
	case errors.Is(err, fs.ErrPermission):
		return newAPIError(http.StatusForbidden, "没有权限")
	default:
		return newAPIError(http.StatusInternalServerError, "服务器内部错误")
	}
}

func contentRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func csrfMatches(sess *session, token string) bool {
	return subtle.ConstantTimeCompare([]byte(sess.csrf), []byte(token)) == 1
}

func (h *Handler) passwordMatches(password string) bool {
	sum := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(sum[:], h.passwordHash[:]) == 1
}

type createdResult struct {
	data any
}

type successEnvelope struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

type errorEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

func writeSuccess(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(successEnvelope{Success: true, Data: data})
}

func writeError(w http.ResponseWriter, apiErr *apiError) {
	if apiErr.allow != "" {
		w.Header().Set("Allow", apiErr.allow)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Success: false, Error: apiErr.message})
}

func validEditablePath(name string, allowRoot bool) error {
	if err := content.ValidatePath(name, allowRoot); err != nil {
		return errInvalidName
	}
	if name == "" {
		return errInvalidName
	}
	if !content.Editable(name) {
		return errInvalidName
	}
	return nil
}

func joinRelative(dir, name string) string {
	if dir == "" {
		return name
	}
	return path.Join(dir, name)
}
