package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"hzk-doc/internal/content"
)

type TreeEntry struct {
	Type             string      `json:"type"`
	Name             string      `json:"name"`
	Path             string      `json:"path"`
	ID               string      `json:"id,omitempty"`
	DisplayName      string      `json:"displayName,omitempty"`
	FileType         string      `json:"fileType,omitempty"`
	FileTypeCategory string      `json:"fileTypeCategory,omitempty"`
	Children         []TreeEntry `json:"children,omitempty"`
}

type DocMeta struct {
	ID        string
	Path      string
	Name      string
	Title     string
	Directory string
	Extension string
	Category  string
	Size      int64
	ModTime   time.Time
}

var docIDPattern = regexp.MustCompile(`(?i)_([0-9]{8}-[a-z]{6})$`)

func (s *Server) scanTree() ([]TreeEntry, map[string]DocMeta, error) {
	docs := make(map[string]DocMeta)
	order := s.loadOrder()
	tree, err := s.scanDir(".", docs, order)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(docs))
	for name := range docs {
		names = append(names, name)
	}
	if err := s.catalog.Register(names); err != nil {
		return nil, nil, err
	}
	active, err := s.catalog.Active()
	if err != nil {
		return nil, nil, err
	}
	for name, meta := range docs {
		meta.ID = active[name].ID
		docs[name] = meta
	}
	var assign func([]TreeEntry)
	assign = func(entries []TreeEntry) {
		for i := range entries {
			if entries[i].Type == "file" {
				entries[i].ID = docs[entries[i].Path].ID
			} else {
				assign(entries[i].Children)
			}
		}
	}
	assign(tree)
	return tree, docs, nil
}

func (s *Server) loadOrder() map[string][]string {
	info, err := regularFileInfo(s.root, content.OrderFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.logger.Warn("检查排序文件失败", "error", err)
		}
		return nil
	}
	if info.Size() > 1<<20 {
		return nil
	}

	data, err := s.root.ReadFile(content.OrderFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.logger.Warn("读取排序文件失败", "error", err)
		}
		return nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var order map[string][]string
	if err := json.Unmarshal(data, &order); err != nil {
		s.logger.Warn("解析排序文件失败", "error", err)
		return nil
	}
	s.catalog.ResolveOrder(order)
	return order
}

func (s *Server) scanDir(dir string, docs map[string]DocMeta, order map[string][]string) ([]TreeEntry, error) {
	entries, err := fs.ReadDir(s.root.FS(), dir)
	if err != nil {
		return nil, err
	}

	nodes := make([]TreeEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}

		relative := name
		if dir != "." {
			relative = path.Join(dir, name)
		}
		if err := content.ValidatePath(relative, false); err != nil {
			continue
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			if strings.EqualFold(name, "assets") {
				continue
			}
			children, err := s.scanDir(relative, docs, order)
			if err != nil {
				s.logger.Warn("跳过无法读取的目录", "path", relative, "error", err)
				continue
			}
			if len(children) == 0 {
				continue
			}
			nodes = append(nodes, TreeEntry{
				Type:     "folder",
				Name:     name,
				Path:     relative,
				Children: children,
			})
			continue
		}

		extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		category := content.Kind(name)
		if category == "" {
			continue
		}

		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		displayName := splitDocName(name)
		directory := path.Dir(relative)
		if directory == "." {
			directory = ""
		}

		meta := DocMeta{
			Path:      relative,
			Name:      name,
			Title:     displayName,
			Directory: directory,
			Extension: extension,
			Category:  category,
			Size:      info.Size(),
			ModTime:   info.ModTime(),
		}
		docs[relative] = meta

		nodes = append(nodes, TreeEntry{
			Type:             "file",
			Name:             name,
			Path:             relative,
			DisplayName:      displayName,
			FileType:         extension,
			FileTypeCategory: category,
		})
	}

	applyTreeOrder(nodes, orderFor(order, dir))
	return nodes, nil
}

func splitDocName(name string) string {
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	match := docIDPattern.FindStringSubmatch(stem)
	if match == nil {
		return stem
	}
	return stem[:len(stem)-len(match[0])]
}

func applyTreeOrder(nodes []TreeEntry, order []string) {
	rank := make(map[string]int, len(order))
	for index, name := range order {
		if _, exists := rank[name]; !exists {
			rank[name] = index
		}
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		left, right := nodes[i], nodes[j]
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
		return treeLess(left, right)
	})
}

func orderFor(order map[string][]string, dir string) []string {
	if dir == "." {
		if names, ok := order[""]; ok {
			return names
		}
		return order["."]
	}
	return order[dir]
}

func treeLess(left, right TreeEntry) bool {
	if left.Type != right.Type {
		return left.Type == "folder"
	}
	if naturalLess(left.Name, right.Name) {
		return true
	}
	if naturalLess(right.Name, left.Name) {
		return false
	}
	return left.Path < right.Path
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
