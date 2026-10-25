package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestCreateDocumentAndSource(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	recorder := env.createDocument("notes/a.md", "# title")
	expectStatus(t, recorder, http.StatusNotFound)
	assertNoPathLeak(t, env.docs, recorder.Body.String())

	expectStatus(t, env.createFolder("notes"), http.StatusCreated)
	recorder = env.createDocument("notes/a.md", "# title")
	expectStatus(t, recorder, http.StatusCreated)
	created := decodeData[documentData](t, recorder)
	if created.Path != "notes/a.md" || len(created.Revision) != 64 {
		t.Fatalf("created = %+v", created)
	}
	if got := readDocFile(t, env.docs, "notes/a.md"); got != "# title" {
		t.Fatalf("file content = %q", got)
	}

	expectStatus(t, env.createDocument("notes/a.md", "again"), http.StatusConflict)
	if got := readDocFile(t, env.docs, "notes/a.md"); got != "# title" {
		t.Fatalf("duplicate create changed file: %q", got)
	}

	source := env.source("notes/a.md")
	if source.Path != "notes/a.md" || source.Content != "# title" || len(source.Revision) != 64 {
		t.Fatalf("source = %+v", source)
	}
	if source.Revision != created.Revision {
		t.Fatalf("source revision = %q, created = %q", source.Revision, created.Revision)
	}

	expectStatus(t, env.createDocument("notes/a.pdf", "pdf"), http.StatusBadRequest)
	expectStatus(t, env.createDocument(".hidden.md", "x"), http.StatusBadRequest)
	expectStatus(t, env.createDocument("../escape.md", "x"), http.StatusBadRequest)
	expectStatus(t, env.createDocument("", "x"), http.StatusBadRequest)

	escapeName := "hzk-escape-" + filepath.Base(env.docs) + ".md"
	escaped := filepath.Join(filepath.Dir(env.docs), escapeName)
	if _, err := os.Stat(escaped); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("traversal created file outside root: %v", err)
	}

	expectStatus(t, env.createFolder("occupied.md"), http.StatusCreated)
	expectStatus(t, env.createDocument("occupied.md", "x"), http.StatusConflict)

	writeDocFile(t, env.docs, "manual.pdf", "pdf")
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/source?path=manual.pdf", nil), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/source?path=missing.md", nil), http.StatusNotFound)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/source?path=", nil), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/source?path=%2Fetc%2Fpasswd", nil), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/document", nil), http.StatusMethodNotAllowed)
	expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/source", nil), http.StatusMethodNotAllowed)
}

func TestEditDocumentRevision(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	expectStatus(t, env.createDocument("edit.md", "one"), http.StatusCreated)
	first := env.source("edit.md")

	recorder := env.editDocument("edit.md", "wrong", "not-the-revision")
	expectStatus(t, recorder, http.StatusConflict)
	if got := readDocFile(t, env.docs, "edit.md"); got != "one" {
		t.Fatalf("conflicting edit changed file: %q", got)
	}

	expectStatus(t, env.editDocument("edit.md", "missing-revision", ""), http.StatusConflict)

	recorder = env.editDocument("edit.md", "two", first.Revision)
	expectStatus(t, recorder, http.StatusOK)
	edited := decodeData[documentData](t, recorder)
	if edited.Revision == first.Revision || len(edited.Revision) != 64 {
		t.Fatalf("edited = %+v", edited)
	}
	second := env.source("edit.md")
	if second.Content != "two" || second.Revision != edited.Revision {
		t.Fatalf("source after edit = %+v", second)
	}

	if err := os.Chmod(filepath.Join(env.docs, "edit.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, env.editDocument("edit.md", "three", second.Revision), http.StatusOK)
	info, err := os.Stat(filepath.Join(env.docs, "edit.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("edit changed permissions to %o", info.Mode().Perm())
	}

	expectStatus(t, env.editDocument("missing.md", "x", "abc"), http.StatusNotFound)
	expectStatus(t, env.editDocument("manual.pdf", "x", "abc"), http.StatusBadRequest)

	expectStatus(t, env.createDocument("race.md", "base"), http.StatusCreated)
	base := env.source("race.md")
	bodyA := jsonBody(t, documentRequest{Path: "race.md", Content: "A", Revision: base.Revision})
	bodyB := jsonBody(t, documentRequest{Path: "race.md", Content: "B", Revision: base.Revision})

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, body := range [][]byte{bodyA, bodyB} {
		wg.Add(1)
		go func(payload []byte) {
			defer wg.Done()
			recorder := env.authRequest(http.MethodPut, "/api/admin/document", payload)
			codes <- recorder.Code
		}(body)
	}
	wg.Wait()
	close(codes)

	successes, conflicts := 0, 0
	for code := range codes {
		switch code {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected concurrent edit status %d", code)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent edit success=%d conflict=%d", successes, conflicts)
	}
	content := readDocFile(t, env.docs, "race.md")
	if content != "A" && content != "B" {
		t.Fatalf("concurrent edit produced mixed content %q", content)
	}

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(env.docs, "link.md")); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/source?path=link.md", nil), http.StatusBadRequest)
	recorder = env.editDocument("link.md", "changed", contentRevision([]byte("outside")))
	expectStatus(t, recorder, http.StatusBadRequest)
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
		t.Fatalf("symlink edit changed outside file: %q, %v", data, err)
	}
}

func TestEntriesListing(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	writeDocFile(t, env.docs, "a2.md", "2")
	writeDocFile(t, env.docs, "a10.md", "10")
	writeDocFile(t, env.docs, "notes.txt", "notes")
	writeDocFile(t, env.docs, ".secret.md", "secret")
	if err := os.MkdirAll(filepath.Join(env.docs, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.docs, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocFile(t, env.docs, "assets/logo.png", "png")
	if err := os.Symlink("a2.md", filepath.Join(env.docs, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.docs, "assets"), filepath.Join(env.docs, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(env.docs, "pipe.txt"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := env.entries("")
	if root.Directory != "" {
		t.Fatalf("directory = %q", root.Directory)
	}
	assertNames(t, root.Entries, []string{"assets", "b", "a2.md", "a10.md", "notes.txt"})

	byName := make(map[string]entryData, len(root.Entries))
	for _, entry := range root.Entries {
		byName[entry.Name] = entry
	}
	if byName["assets"].Type != "folder" || byName["assets"].Kind != "folder" {
		t.Fatalf("assets entry = %+v", byName["assets"])
	}
	if byName["a2.md"].Kind != "markdown" || !byName["a2.md"].Editable || byName["a2.md"].Size != 1 {
		t.Fatalf("a2 entry = %+v", byName["a2.md"])
	}
	if byName["notes.txt"].Kind != "text" || !byName["notes.txt"].Editable {
		t.Fatalf("notes entry = %+v", byName["notes.txt"])
	}
	if byName["assets"].ModifiedAt.IsZero() {
		t.Fatal("folder has zero modifiedAt")
	}

	empty := env.entries("b")
	if len(empty.Entries) != 0 || empty.Entries == nil {
		t.Fatalf("empty entries = %#v", empty.Entries)
	}

	assets := env.entries("assets")
	assertNames(t, assets.Entries, []string{"logo.png"})
	if assets.Entries[0].Kind != "image" || assets.Entries[0].Editable {
		t.Fatalf("image entry = %+v", assets.Entries[0])
	}

	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/entries?dir=missing", nil), http.StatusNotFound)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/entries?dir=a2.md", nil), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/entries?dir=.", nil), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/entries?dir=..%2F", nil), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/entries?dir=linked", nil), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/source?path=pipe.txt", nil), http.StatusBadRequest)
}

func TestFolderCreation(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	expectStatus(t, env.createFolder("a"), http.StatusCreated)
	if info, err := os.Stat(filepath.Join(env.docs, "a")); err != nil || !info.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	expectStatus(t, env.createFolder("a/b"), http.StatusCreated)
	expectStatus(t, env.createFolder("a"), http.StatusConflict)
	expectStatus(t, env.createFolder("missing/c"), http.StatusNotFound)
	expectStatus(t, env.createFolder(".hidden"), http.StatusBadRequest)
	expectStatus(t, env.createFolder(""), http.StatusBadRequest)
	expectStatus(t, env.createFolder("a/../z"), http.StatusBadRequest)
	expectStatus(t, env.createFolder("a "), http.StatusBadRequest)

	writeDocFile(t, env.docs, "a/block.md", "x")
	expectStatus(t, env.createFolder("a/block.md/c"), http.StatusBadRequest)
}

func TestUpload(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	expectStatus(t, env.createFolder("assets"), http.StatusCreated)
	env.changes.Store(0)

	recorder := env.upload("logo.png", "assets", []byte("image-data"), true)
	expectStatus(t, recorder, http.StatusCreated)
	uploaded := decodeData[uploadData](t, recorder)
	if uploaded.Path != "assets/logo.png" || uploaded.Size != int64(len("image-data")) {
		t.Fatalf("uploaded = %+v", uploaded)
	}
	if got := readDocFile(t, env.docs, "assets/logo.png"); got != "image-data" {
		t.Fatalf("upload content = %q", got)
	}

	expectStatus(t, env.upload("logo.png", "assets", []byte("replacement"), true), http.StatusConflict)
	if got := readDocFile(t, env.docs, "assets/logo.png"); got != "image-data" {
		t.Fatalf("overwrite changed content: %q", got)
	}

	recorder = env.upload("root.txt", "", []byte("root"), true)
	expectStatus(t, recorder, http.StatusCreated)
	if got := readDocFile(t, env.docs, "root.txt"); got != "root" {
		t.Fatalf("root upload content = %q", got)
	}

	expectStatus(t, env.upload("nodir.txt", "", []byte("x"), false), http.StatusBadRequest)

	leakName := "leak-" + filepath.Base(env.docs) + ".png"
	expectStatus(t, env.upload("../"+leakName, "assets", []byte("x"), true), http.StatusBadRequest)
	if _, err := os.Stat(filepath.Join(env.docs, "..", leakName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("traversal upload created file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.docs, "assets", leakName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("traversal upload reached target: %v", err)
	}

	expectStatus(t, env.upload("sub/evil.png", "assets", []byte("x"), true), http.StatusBadRequest)
	expectStatus(t, env.upload(`..\evil.png`, "assets", []byte("x"), true), http.StatusBadRequest)
	expectStatus(t, env.upload(".evil.png", "assets", []byte("x"), true), http.StatusBadRequest)
	expectStatus(t, env.upload("evil.exe", "assets", []byte("x"), true), http.StatusBadRequest)
	expectStatus(t, env.upload("ok.png", "missing", []byte("x"), true), http.StatusNotFound)
	expectStatus(t, env.upload("ok.png", ".hidden", []byte("x"), true), http.StatusBadRequest)

	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(env.docs, "linkassets")); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, env.upload("outside.png", "linkassets", []byte("x"), true), http.StatusBadRequest)
	if _, err := os.Stat(filepath.Join(outsideDir, "outside.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlinked upload escaped root: %v", err)
	}

	var duplicateBuffer bytes.Buffer
	writer := multipart.NewWriter(&duplicateBuffer)
	if err := writer.WriteField("directory", "assets"); err != nil {
		t.Fatal(err)
	}
	firstPart, err := writer.CreateFormFile("file", "first.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstPart.Write([]byte("first")); err != nil {
		t.Fatal(err)
	}
	secondPart, err := writer.CreateFormFile("file", "second.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secondPart.Write([]byte("second")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	recorder = env.authRequest(http.MethodPost, "/api/admin/upload", duplicateBuffer.Bytes(), func(r *http.Request) {
		r.Header.Set("Content-Type", writer.FormDataContentType())
	})
	expectStatus(t, recorder, http.StatusBadRequest)
	for _, name := range []string{"first.png", "second.png"} {
		if _, err := os.Stat(filepath.Join(env.docs, "assets", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("multi-file upload created %s: %v", name, err)
		}
	}

	var bigBuffer bytes.Buffer
	bigWriter := multipart.NewWriter(&bigBuffer)
	if err := bigWriter.WriteField("directory", "assets"); err != nil {
		t.Fatal(err)
	}
	bigPart, err := bigWriter.CreateFormFile("file", "big.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(bigPart, zeroReader{}, maxUploadBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := bigWriter.Close(); err != nil {
		t.Fatal(err)
	}
	recorder = env.authRequest(http.MethodPost, "/api/admin/upload", bigBuffer.Bytes(), func(r *http.Request) {
		r.Header.Set("Content-Type", bigWriter.FormDataContentType())
	})
	expectStatus(t, recorder, http.StatusRequestEntityTooLarge)
	if _, err := os.Stat(filepath.Join(env.docs, "assets", "big.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized upload created file: %v", err)
	}

	if got := env.changes.Load(); got != 2 {
		t.Fatalf("OnChange calls = %d, want 2", got)
	}
}

func TestPreview(t *testing.T) {
	var (
		gotPath   string
		gotSource string
		calls     int
	)
	env := newTestEnv(t, func(cfg *Config) {
		cfg.Preview = func(documentPath, source string) (string, error) {
			gotPath = documentPath
			gotSource = source
			calls++
			return "<p>rendered</p>", nil
		}
	})
	env.mustLogin()

	body := jsonBody(t, previewRequest{Path: "a.md", Content: "# heading"})
	recorder := env.authRequest(http.MethodPost, "/api/admin/preview", body)
	expectStatus(t, recorder, http.StatusOK)
	preview := decodeData[previewData](t, recorder)
	if preview.Content != "<p>rendered</p>" || gotPath != "a.md" || gotSource != "# heading" || calls != 1 {
		t.Fatalf("preview = %+v, path=%q source=%q calls=%d", preview, gotPath, gotSource, calls)
	}

	recorder = env.authRequest(http.MethodPost, "/api/admin/preview", body, func(r *http.Request) {
		r.Header.Del("X-CSRF-Token")
	})
	expectStatus(t, recorder, http.StatusForbidden)
	if calls != 1 {
		t.Fatal("preview called without CSRF token")
	}

	recorder = env.authRequest(http.MethodPost, "/api/admin/preview", body, func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	expectStatus(t, recorder, http.StatusForbidden)
	if calls != 1 {
		t.Fatal("preview called with invalid origin")
	}

	expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/preview",
		jsonBody(t, previewRequest{Path: "a.pdf", Content: "x"})), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/preview",
		jsonBody(t, previewRequest{Path: "../a.md", Content: "x"})), http.StatusBadRequest)
	if calls != 1 {
		t.Fatal("preview called for invalid path")
	}

	env.h.preview = func(string, string) (string, error) {
		return "", errors.New("打开 /secret/absolute/path.md 失败")
	}
	recorder = env.authRequest(http.MethodPost, "/api/admin/preview", body)
	expectStatus(t, recorder, http.StatusInternalServerError)
	if strings.Contains(recorder.Body.String(), "/secret/absolute/path.md") {
		t.Fatal("preview error leaked absolute path")
	}

	env.h.preview = nil
	expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/preview", body), http.StatusServiceUnavailable)

	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/preview", nil), http.StatusMethodNotAllowed)
}

func TestOnChangeBehavior(t *testing.T) {
	env := newTestEnv(t, func(cfg *Config) {
		cfg.Preview = func(string, string) (string, error) { return "<p>x</p>", nil }
	})
	env.mustLogin()
	env.changes.Store(0)

	expectStatus(t, env.createFolder("assets"), http.StatusCreated)
	expectStatus(t, env.createDocument("a.md", "one"), http.StatusCreated)
	revision := env.source("a.md").Revision
	expectStatus(t, env.editDocument("a.md", "two", revision), http.StatusOK)
	expectStatus(t, env.upload("logo.png", "assets", []byte("png"), true), http.StatusCreated)
	expectStatus(t, env.order("", []string{"a.md", "assets"}), http.StatusOK)

	if got := env.changes.Load(); got != 5 {
		t.Fatalf("OnChange calls after writes = %d, want 5", got)
	}

	expectStatus(t, env.createDocument("a.md", "duplicate"), http.StatusConflict)
	expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/preview",
		jsonBody(t, previewRequest{Path: "a.md", Content: "# x"})), http.StatusOK)
	if got := env.changes.Load(); got != 5 {
		t.Fatalf("OnChange called for failed write or preview: %d", got)
	}

	failing := newTestEnv(t, func(cfg *Config) {
		cfg.OnChange = func() error { return errors.New("刷新失败") }
	})
	failing.mustLogin()
	recorder := failing.createFolder("after-error")
	expectStatus(t, recorder, http.StatusInternalServerError)
	assertNoPathLeak(t, failing.docs, recorder.Body.String())
	if info, err := os.Stat(filepath.Join(failing.docs, "after-error")); err != nil || !info.IsDir() {
		t.Fatalf("write was not committed before refresh error: %v", err)
	}
}

func TestSymlinkSecurity(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.md")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(env.docs, "leak.md")); err != nil {
		t.Fatal(err)
	}

	assertNames(t, env.entries("").Entries, []string{})

	recorder := env.authRequest(http.MethodGet, "/api/admin/source?path=leak.md", nil)
	expectStatus(t, recorder, http.StatusBadRequest)
	assertNoPathLeak(t, outsideDir, recorder.Body.String())

	expectStatus(t, env.editDocument("leak.md", "changed", contentRevision([]byte("outside"))), http.StatusBadRequest)
	if data, err := os.ReadFile(outsideFile); err != nil || string(data) != "outside" {
		t.Fatalf("symlink edit changed outside file: %q, %v", data, err)
	}

	expectStatus(t, env.createDocument("leak.md", "x"), http.StatusConflict)
	expectStatus(t, env.createFolder("leak.md"), http.StatusConflict)
	expectStatus(t, env.upload("leak.md", "", []byte("x"), true), http.StatusConflict)
	if data, err := os.ReadFile(outsideFile); err != nil || string(data) != "outside" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}

	writeDocFile(t, env.docs, ".private.md", "private")
	if err := os.Symlink(".private.md", filepath.Join(env.docs, "visible.md")); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/source?path=visible.md", nil), http.StatusBadRequest)
	expectStatus(t, env.upload("visible.md", "", []byte("x"), true), http.StatusConflict)
	if got := readDocFile(t, env.docs, ".private.md"); got != "private" {
		t.Fatalf("internal symlink target changed: %q", got)
	}

	if err := os.Mkdir(filepath.Join(env.docs, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.docs, "assets"), filepath.Join(env.docs, "linked")); err != nil {
		t.Fatal(err)
	}
	assertNames(t, env.entries("").Entries, []string{"assets"})
	expectStatus(t, env.createFolder("linked/new"), http.StatusBadRequest)
	expectStatus(t, env.upload("outside.png", "linked", []byte("x"), true), http.StatusBadRequest)
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/entries?dir=linked", nil), http.StatusBadRequest)
}

func TestErrorsDoNotLeakPathsOrSecrets(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	responses := []*httptest.ResponseRecorder{
		env.authRequest(http.MethodGet, "/api/admin/source?path=missing.md", nil),
		env.authRequest(http.MethodGet, "/api/admin/entries?dir=missing", nil),
		env.upload("x.png", "missing", []byte("x"), true),
		env.authRequest(http.MethodPost, "/api/admin/document",
			jsonBody(t, documentRequest{Path: "../x.md", Content: "x"})),
	}
	for _, recorder := range responses {
		assertNoPathLeak(t, env.docs, recorder.Body.String())
	}

	expectStatus(t, env.createDocument("dup.md", "x"), http.StatusCreated)
	recorder := env.createDocument("dup.md", "x")
	expectStatus(t, recorder, http.StatusConflict)
	assertNoPathLeak(t, env.docs, recorder.Body.String())

	recorder = env.login("wrong-password")
	expectStatus(t, recorder, http.StatusUnauthorized)
	if strings.Contains(recorder.Body.String(), "wrong-password") {
		t.Fatal("login error leaked password")
	}
}

func TestCreationStatusesOverHTTP(t *testing.T) {
	env := newTestEnv(t, nil)

	mux := http.NewServeMux()
	mux.Handle("/api/admin/", env.h)
	app := httptest.NewServer(mux)
	defer app.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	request := func(method, requestPath string, body io.Reader, contentType, csrf string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, app.URL+requestPath, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", app.URL)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}

	status, body := request(http.MethodPost, "/api/admin/login",
		bytes.NewReader(jsonBody(t, map[string]string{"password": testPassword})), "application/json", "")
	if status != http.StatusOK {
		t.Fatalf("login status = %d", status)
	}
	var login struct {
		Success bool `json:"success"`
		Data    struct {
			CSRFToken string `json:"csrfToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &login); err != nil || !login.Success || login.Data.CSRFToken == "" {
		t.Fatalf("login response = %s, %v", body, err)
	}
	csrf := login.Data.CSRFToken

	status, _ = request(http.MethodPost, "/api/admin/document",
		bytes.NewReader(jsonBody(t, documentRequest{Path: "http-status.md", Content: "one"})),
		"application/json", csrf)
	if status != http.StatusCreated {
		t.Fatalf("document create status = %d", status)
	}

	status, _ = request(http.MethodPost, "/api/admin/folder",
		bytes.NewReader(jsonBody(t, folderRequest{Path: "folder"})), "application/json", csrf)
	if status != http.StatusCreated {
		t.Fatalf("folder create status = %d", status)
	}

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	if err := writer.WriteField("directory", "folder"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("png")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	status, _ = request(http.MethodPost, "/api/admin/upload", bytes.NewReader(upload.Bytes()), writer.FormDataContentType(), csrf)
	if status != http.StatusCreated {
		t.Fatalf("upload status = %d", status)
	}
	if got := readDocFile(t, env.docs, "folder/image.png"); got != "png" {
		t.Fatalf("uploaded content = %q", got)
	}

	status, body = request(http.MethodGet, "/api/admin/source?path=http-status.md", nil, "", "")
	if status != http.StatusOK {
		t.Fatalf("source status = %d", status)
	}
	var source struct {
		Data struct {
			Revision string `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &source); err != nil || source.Data.Revision == "" {
		t.Fatalf("source response = %s, %v", body, err)
	}
	revision := source.Data.Revision
	status, _ = request(http.MethodPut, "/api/admin/document",
		bytes.NewReader(jsonBody(t, documentRequest{Path: "http-status.md", Content: "two", Revision: revision})),
		"application/json", csrf)
	if status != http.StatusOK {
		t.Fatalf("document edit status = %d", status)
	}

	status, _ = request(http.MethodPut, "/api/admin/order",
		bytes.NewReader(jsonBody(t, orderRequest{Directory: "", Names: []string{"http-status.md", "folder"}})),
		"application/json", csrf)
	if status != http.StatusOK {
		t.Fatalf("order status = %d", status)
	}

	status, _ = request(http.MethodPost, "/api/admin/logout", nil, "", csrf)
	if status != http.StatusOK {
		t.Fatalf("logout status = %d", status)
	}
}
