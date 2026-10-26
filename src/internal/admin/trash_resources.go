package admin

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"hzk-doc/internal/catalog"
)

// 只有文件系统确认是同一文件时才改用磁盘名称，不能直接转小写。
func (h *Handler) canonicalPath(name string) (string, error) {
	if _, err := h.lstatPath(name); err != nil {
		return "", err
	}
	directory := "."
	for _, part := range strings.Split(name, "/") {
		entries, err := fs.ReadDir(h.root.FS(), directory)
		if err != nil {
			return "", err
		}
		matched := ""
		for _, entry := range entries {
			if entry.Name() == part {
				matched = part
				break
			}
		}
		if matched == "" {
			target, err := h.root.Lstat(path.Join(directory, part))
			if err != nil {
				return "", err
			}
			for _, entry := range entries {
				info, err := entry.Info()
				if err == nil && os.SameFile(info, target) {
					matched = entry.Name()
					break
				}
			}
		}
		if matched == "" {
			return "", fs.ErrNotExist
		}
		directory = path.Join(directory, matched)
	}
	return directory, nil
}

func (h *Handler) retainedFolderAssets(directory string) (map[string]bool, error) {
	inside := func(name string) bool { return name == directory || strings.HasPrefix(name, directory+"/") }
	refs, complete, err := h.documentReferences(inside, nil)
	if err != nil {
		return nil, err
	}
	trashed, trashComplete, err := h.trashReferences(nil, nil)
	if err != nil {
		return nil, err
	}
	if !complete || !trashComplete {
		return nil, errors.New("无法完整检查素材引用")
	}
	for name := range trashed {
		refs[name] = true
	}
	var pending []string
	for name := range refs {
		pending = append(pending, name)
	}
	retained := make(map[string]bool)
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if !inside(name) || !removableAsset(name) || retained[name] {
			continue
		}
		info, err := h.lstatPath(name)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		retained[name] = true
		if strings.EqualFold(path.Ext(name), ".css") {
			data, err := h.readReferenceSource(name)
			if err != nil {
				return nil, err
			}
			pending = append(pending, h.resourceReferences(data, name)...)
		}
	}
	return retained, nil
}

func (h *Handler) trashReferences(selected map[string]bool, collect func(string, []byte)) (map[string]bool, bool, error) {
	entries, err := h.catalog.TrashList()
	if err != nil {
		return nil, false, err
	}
	refs := make(map[string]bool)
	complete := true
	for _, entry := range entries {
		prefix := catalog.TrashPath(entry.ID) + "/files/"
		for _, item := range entry.Items {
			err := fs.WalkDir(h.root.FS(), prefix+item.Path, func(name string, file fs.DirEntry, err error) error {
				if err != nil {
					complete = false
					return nil
				}
				if !file.Type().IsRegular() || !referencingDocument(name) {
					return nil
				}
				data, err := h.readReferenceSource(name)
				if err != nil {
					complete = false
					return nil
				}
				original := strings.TrimPrefix(name, prefix)
				if selected[entry.ID] {
					if collect != nil {
						collect(original, data)
					}
					return nil
				}
				for _, ref := range h.resourceReferences(data, original) {
					refs[ref] = true
				}
				return nil
			})
			if err != nil {
				return nil, false, err
			}
		}
	}
	return refs, complete, nil
}

func (h *Handler) purgeAssets(entries []trashEntry) ([]trashItem, error) {
	selected := make(map[string]bool, len(entries))
	for _, entry := range entries {
		selected[entry.ID] = true
	}
	candidates := make(map[string]bool)
	remaining, complete, err := h.trashReferences(selected, func(name string, data []byte) {
		for _, ref := range h.resourceReferences(data, name) {
			if removableAsset(ref) {
				candidates[ref] = true
			}
		}
	})
	if err != nil || !complete {
		return nil, err
	}
	active, complete, err := h.documentReferences(func(string) bool { return false }, nil)
	if err != nil || !complete {
		return nil, err
	}
	var names []string
	for name := range candidates {
		if !remaining[name] && !active[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	items := make([]trashItem, 0, len(names))
	for _, name := range names {
		info, err := h.lstatPath(name)
		if err == nil && info.Mode().IsRegular() {
			items = append(items, trashItem{Path: name, Type: "file", Size: info.Size()})
		}
	}
	return items, nil
}
