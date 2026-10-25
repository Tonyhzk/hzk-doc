package admin

import (
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"hzk-doc/internal/content"
)

type entryData struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Kind       string    `json:"kind"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Editable   bool      `json:"editable"`
}

type entriesData struct {
	Directory string      `json:"directory"`
	Entries   []entryData `json:"entries"`
}

func (h *Handler) handleEntries(r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireSession(r); apiErr != nil {
		return nil, apiErr
	}

	dir := r.URL.Query().Get("dir")
	if err := content.ValidatePath(dir, true); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}

	h.files.RLock()
	defer h.files.RUnlock()

	entries, err := h.listEntries(dir)
	if err != nil {
		return nil, apiErrorFor(err)
	}
	return entriesData{Directory: dir, Entries: entries}, nil
}

func (h *Handler) listEntries(dir string) ([]entryData, error) {
	directory, err := h.openDir(dir)
	if err != nil {
		return nil, err
	}
	defer directory.Close()

	items, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	order, _ := h.loadOrder()
	entries := make([]entryData, 0, len(items))
	for _, item := range items {
		name := item.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if item.Type()&fs.ModeSymlink != 0 {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			continue
		}

		isDir := info.IsDir()
		if !isDir && !info.Mode().IsRegular() {
			continue
		}

		entry := entryData{
			Name:       name,
			Path:       joinRelative(dir, name),
			ModifiedAt: info.ModTime().UTC(),
		}
		if isDir {
			entry.Type = "folder"
			entry.Kind = "folder"
		} else {
			entry.Type = "file"
			entry.Kind = content.Kind(name)
			entry.Size = info.Size()
			entry.Editable = content.Editable(name)
		}
		entries = append(entries, entry)
	}

	applyOrder(entries, order[dir])
	return entries, nil
}

func applyOrder(entries []entryData, order []string) {
	rank := make(map[string]int, len(order))
	for i, name := range order {
		if _, exists := rank[name]; !exists {
			rank[name] = i
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		leftRank, leftOrdered := rank[left.Name]
		rightRank, rightOrdered := rank[right.Name]
		switch {
		case leftOrdered && rightOrdered:
			return leftRank < rightRank
		case leftOrdered:
			return true
		case rightOrdered:
			return false
		}
		return entryLess(left, right)
	})
}

func entryLess(left, right entryData) bool {
	if left.Type != right.Type {
		return left.Type == "folder"
	}
	if naturalLess(left.Name, right.Name) {
		return true
	}
	if naturalLess(right.Name, left.Name) {
		return false
	}
	return left.Name < right.Name
}

func naturalLess(left, right string) bool {
	if left == right {
		return false
	}
	a := []rune(strings.ToLower(left))
	b := []rune(strings.ToLower(right))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		if unicode.IsDigit(a[i]) && unicode.IsDigit(b[j]) {
			aStart, bStart := i, j
			for i < len(a) && unicode.IsDigit(a[i]) {
				i++
			}
			for j < len(b) && unicode.IsDigit(b[j]) {
				j++
			}
			aNumber := strings.TrimLeft(string(a[aStart:i]), "0")
			bNumber := strings.TrimLeft(string(b[bStart:j]), "0")
			if aNumber == "" {
				aNumber = "0"
			}
			if bNumber == "" {
				bNumber = "0"
			}
			if len(aNumber) != len(bNumber) {
				return len(aNumber) < len(bNumber)
			}
			if aNumber != bNumber {
				return aNumber < bNumber
			}
			continue
		}

		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a) < len(b)
}
