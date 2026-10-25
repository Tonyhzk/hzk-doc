package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"hzk-doc/internal/content"
)

func readOrderMetadata(t *testing.T, docs string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docs, content.OrderFile))
	if err != nil {
		t.Fatalf("read order metadata: %v", err)
	}
	var order map[string][]string
	if err := json.Unmarshal(data, &order); err != nil {
		t.Fatalf("parse order metadata: %v", err)
	}
	return order
}

func TestOrderPersistsAndReloads(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	expectStatus(t, env.createFolder("folder"), http.StatusCreated)
	expectStatus(t, env.createDocument("b.md", "b"), http.StatusCreated)
	expectStatus(t, env.createDocument("a.md", "a"), http.StatusCreated)

	assertNames(t, env.entries("").Entries, []string{"folder", "a.md", "b.md"})

	expectStatus(t, env.order("", []string{"b.md", "folder", "a.md"}), http.StatusOK)
	assertNames(t, env.entries("").Entries, []string{"b.md", "folder", "a.md"})

	metadata := readOrderMetadata(t, env.docs)
	if got := metadata[""]; len(got) != 3 || got[0] != "b.md" || got[1] != "folder" || got[2] != "a.md" {
		t.Fatalf("root order metadata = %v", metadata)
	}

	expectStatus(t, env.createDocument("folder/x.md", "x"), http.StatusCreated)
	expectStatus(t, env.createDocument("folder/y.md", "y"), http.StatusCreated)
	expectStatus(t, env.order("folder", []string{"y.md", "x.md"}), http.StatusOK)
	assertNames(t, env.entries("folder").Entries, []string{"y.md", "x.md"})

	metadata = readOrderMetadata(t, env.docs)
	if got := metadata["folder"]; len(got) != 2 || got[0] != "y.md" || got[1] != "x.md" {
		t.Fatalf("folder order metadata = %v", metadata)
	}

	if err := env.h.Close(); err != nil {
		t.Fatalf("close handler: %v", err)
	}
	reopened, err := New(Config{DocsDir: env.docs, Password: testPassword})
	if err != nil {
		t.Fatalf("reopen handler: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reopenedEnv := &testEnv{t: t, docs: env.docs, h: reopened, password: testPassword}
	reopenedEnv.mustLogin()
	assertNames(t, reopenedEnv.entries("").Entries, []string{"b.md", "folder", "a.md"})
	assertNames(t, reopenedEnv.entries("folder").Entries, []string{"y.md", "x.md"})
}

func TestOrderValidation(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	expectStatus(t, env.createFolder("c"), http.StatusCreated)
	expectStatus(t, env.createDocument("a.md", "a"), http.StatusCreated)
	expectStatus(t, env.createDocument("b.md", "b"), http.StatusCreated)

	valid := []string{"a.md", "b.md", "c"}
	cases := [][]string{
		{"a.md", "a.md", "b.md", "c"},
		{"a.md", "b.md"},
		{"a.md", "b.md", "c", "extra.md"},
		{".hidden", "a.md", "b.md", "c"},
		{"a/b.md", "a.md", "b.md", "c"},
		{"", "a.md", "b.md", "c"},
		{"a.md", "b.md", "c "},
	}
	for _, names := range cases {
		expectStatus(t, env.order("", names), http.StatusBadRequest)
	}
	expectStatus(t, env.order("", nil), http.StatusBadRequest)
	expectStatus(t, env.order("missing", nil), http.StatusNotFound)
	expectStatus(t, env.order("../outside", nil), http.StatusBadRequest)
	expectStatus(t, env.order(".", nil), http.StatusBadRequest)

	if _, err := os.Stat(filepath.Join(env.docs, content.OrderFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid order requests created metadata: %v", err)
	}

	expectStatus(t, env.order("", valid), http.StatusOK)
	assertNames(t, env.entries("").Entries, valid)

	if err := os.WriteFile(filepath.Join(env.docs, content.OrderFile), []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, env.order("", valid), http.StatusOK)
	assertNames(t, env.entries("").Entries, valid)

	if err := os.WriteFile(filepath.Join(env.docs, content.OrderFile), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	recorder := env.order("", valid)
	expectStatus(t, recorder, http.StatusInternalServerError)
	assertNoPathLeak(t, env.docs, recorder.Body.String())
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/entries", nil), http.StatusOK)

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "order.json")
	if err := os.WriteFile(outside, []byte(`{"":["a.md"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(env.docs, content.OrderFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(env.docs, content.OrderFile)); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, env.order("", valid), http.StatusInternalServerError)
	if data, err := os.ReadFile(outside); err != nil || string(data) != `{"":["a.md"]}` {
		t.Fatalf("order symlink target changed: %q, %v", data, err)
	}
	if err := os.Remove(filepath.Join(env.docs, content.OrderFile)); err != nil {
		t.Fatal(err)
	}

	if err := syscall.Mkfifo(filepath.Join(env.docs, content.OrderFile), 0o644); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, env.order("", valid), http.StatusInternalServerError)
}

func TestOrderEmptyDirectory(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	expectStatus(t, env.createFolder("empty"), http.StatusCreated)

	recorder := env.order("empty", []string{})
	expectStatus(t, recorder, http.StatusOK)
	data := decodeData[orderData](t, recorder)
	if data.Names == nil || len(data.Names) != 0 {
		t.Fatalf("empty order data = %+v", data)
	}

	raw, err := os.ReadFile(filepath.Join(env.docs, content.OrderFile))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if string(metadata["empty"]) != "[]" {
		t.Fatalf("empty order metadata = %s", metadata["empty"])
	}
	if len(env.entries("empty").Entries) != 0 {
		t.Fatal("empty directory has entries")
	}
}

func TestOrderConcurrent(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()

	names := make([]string, 0, 12)
	for i := range 12 {
		name := "file-" + string(rune('a'+i)) + ".md"
		writeDocFile(t, env.docs, name, "x")
		names = append(names, name)
	}
	reversed := make([]string, len(names))
	for i := range names {
		reversed[len(names)-1-i] = names[i]
	}

	orders := [][]string{names, reversed}
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := range 8 {
		wg.Add(1)
		go func(order []string) {
			defer wg.Done()
			codes <- env.order("", order).Code
		}(orders[i%len(orders)])
	}
	wg.Wait()
	close(codes)

	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent order status = %d", code)
		}
	}

	finalEntries := env.entries("").Entries
	got := namesOf(finalEntries)
	isForward := equalNames(got, names)
	isReversed := equalNames(got, reversed)
	if !isForward && !isReversed {
		t.Fatalf("final order = %v", got)
	}

	metadata := readOrderMetadata(t, env.docs)
	if !equalNames(metadata[""], got) {
		t.Fatalf("metadata = %v, entries = %v", metadata[""], got)
	}
}

func equalNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
