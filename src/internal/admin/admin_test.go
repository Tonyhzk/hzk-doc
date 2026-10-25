package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testPassword = "correct-horse-battery-staple"

type testEnv struct {
	t        *testing.T
	docs     string
	h        *Handler
	password string
	token    string
	csrf     string
	changes  atomic.Int64
}

func newTestEnv(t *testing.T, configure func(*Config)) *testEnv {
	t.Helper()

	docs := t.TempDir()
	env := &testEnv{t: t, docs: docs}
	cfg := Config{
		DocsDir:  docs,
		Password: testPassword,
		OnChange: func() error {
			env.changes.Add(1)
			return nil
		},
	}
	if configure != nil {
		configure(&cfg)
	}
	env.password = cfg.Password

	handler, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env.h = handler
	t.Cleanup(func() {
		_ = handler.Close()
	})
	return env
}

type requestOption func(*http.Request)

func (e *testEnv) request(method, target string, body []byte, options ...requestOption) *httptest.ResponseRecorder {
	e.t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, "http://example.com"+target, reader)
	req.Header.Set("Origin", "http://example.com")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, option := range options {
		option(req)
	}

	recorder := httptest.NewRecorder()
	e.h.ServeHTTP(recorder, req)
	return recorder
}

func (e *testEnv) authRequest(method, target string, body []byte, options ...requestOption) *httptest.ResponseRecorder {
	e.t.Helper()

	options = append([]requestOption{
		func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: e.token})
			if body != nil {
				r.Header.Set("X-CSRF-Token", e.csrf)
			}
		},
	}, options...)
	return e.request(method, target, body, options...)
}

func (e *testEnv) login(password string) *httptest.ResponseRecorder {
	e.t.Helper()
	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		e.t.Fatalf("marshal login: %v", err)
	}
	return e.request(http.MethodPost, "/api/admin/login", body)
}

func (e *testEnv) mustLogin() {
	e.t.Helper()

	recorder := e.login(e.password)
	if recorder.Code != http.StatusOK {
		e.t.Fatalf("login status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	data := decodeData[sessionData](e.t, recorder)
	if !data.Authenticated || data.CSRFToken == "" {
		e.t.Fatalf("login data = %+v", data)
	}
	cookie := findCookie(recorder.Result().Cookies(), cookieName)
	if cookie == nil {
		e.t.Fatal("login did not set session cookie")
	}
	e.token = cookie.Value
	e.csrf = data.CSRFToken
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) apiEnvelope {
	t.Helper()
	var envelope apiEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v, body = %s", err, recorder.Body.String())
	}
	return envelope
}

func decodeData[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	envelope := decodeEnvelope(t, recorder)
	if !envelope.Success {
		t.Fatalf("request failed: %s", envelope.Error)
	}
	var value T
	if err := json.Unmarshal(envelope.Data, &value); err != nil {
		t.Fatalf("decode data: %v, data = %s", err, envelope.Data)
	}
	return value
}

func expectStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int) apiEnvelope {
	t.Helper()
	envelope := decodeEnvelope(t, recorder)
	if recorder.Code != want {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, want, recorder.Body.String())
	}
	if want >= 400 {
		if envelope.Success {
			t.Fatalf("error response marked success: %s", recorder.Body.String())
		}
		if envelope.Error == "" {
			t.Fatalf("error response has no message: %s", recorder.Body.String())
		}
	} else if !envelope.Success {
		t.Fatalf("success response marked failure: %s", recorder.Body.String())
	}
	return envelope
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func jsonBody(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func (e *testEnv) createDocument(documentPath, content string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := jsonBody(e.t, documentRequest{Path: documentPath, Content: content})
	return e.authRequest(http.MethodPost, "/api/admin/document", body)
}

func (e *testEnv) editDocument(documentPath, content, revision string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := jsonBody(e.t, documentRequest{Path: documentPath, Content: content, Revision: revision})
	return e.authRequest(http.MethodPut, "/api/admin/document", body)
}

func (e *testEnv) createFolder(folderPath string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := jsonBody(e.t, folderRequest{Path: folderPath})
	return e.authRequest(http.MethodPost, "/api/admin/folder", body)
}

func (e *testEnv) source(documentPath string) sourceData {
	e.t.Helper()
	recorder := e.authRequest(http.MethodGet, "/api/admin/source?path="+url.QueryEscape(documentPath), nil)
	return decodeData[sourceData](e.t, recorder)
}

func (e *testEnv) entries(dir string) entriesData {
	e.t.Helper()
	recorder := e.authRequest(http.MethodGet, "/api/admin/entries?dir="+url.QueryEscape(dir), nil)
	return decodeData[entriesData](e.t, recorder)
}

func (e *testEnv) order(directory string, names []string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := jsonBody(e.t, orderRequest{Directory: directory, Names: names})
	return e.authRequest(http.MethodPut, "/api/admin/order", body)
}

func (e *testEnv) upload(filename, directory string, data []byte, includeDirectory bool) *httptest.ResponseRecorder {
	e.t.Helper()

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if includeDirectory {
		if err := writer.WriteField("directory", directory); err != nil {
			e.t.Fatalf("write directory field: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		e.t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		e.t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		e.t.Fatalf("close multipart writer: %v", err)
	}

	return e.authRequest(http.MethodPost, "/api/admin/upload", buffer.Bytes(), func(r *http.Request) {
		r.Header.Set("Content-Type", writer.FormDataContentType())
	})
}

func writeDocFile(t *testing.T, docs, name, content string) {
	t.Helper()
	full := filepath.Join(docs, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func readDocFile(t *testing.T, docs, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docs, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return string(data)
}

func namesOf(entries []entryData) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}

func assertNames(t *testing.T, entries []entryData, want []string) {
	t.Helper()
	got := namesOf(entries)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("names = %q, want %q", got, want)
	}
}

func assertNoPathLeak(t *testing.T, docs string, body string) {
	t.Helper()
	if strings.Contains(body, docs) {
		t.Fatalf("response leaked document path: %s", body)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestCloseIsIdempotent(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	if err := env.h.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	expectStatus(t, env.request(http.MethodGet, "/api/admin/session", nil), http.StatusServiceUnavailable)
	if err := env.h.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
