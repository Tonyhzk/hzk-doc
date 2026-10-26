package admin

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"hzk-doc/internal/content"
)

const (
	maxReferenceBytes = 8 << 20
	trashEntriesDir   = content.TrashDir + "/entries"
	trashFilesDir     = "files"
	trashMetaFile     = "meta.json"
)

var trashIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[A-Za-z0-9_-]{6,32}$`)

type trashItem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type trashMeta struct {
	DeletedAt time.Time   `json:"deletedAt"`
	Items     []trashItem `json:"items"`
}

type trashEntry struct {
	ID        string      `json:"id"`
	DeletedAt time.Time   `json:"deletedAt"`
	Items     []trashItem `json:"items"`
	Size      int64       `json:"size"`
}

type trashData struct {
	Entries []trashEntry `json:"entries"`
	Size    int64        `json:"size"`
}

type deleteRequest struct {
	Path string `json:"path"`
}

type trashIDsRequest struct {
	IDs []string `json:"ids"`
}

type trashActionResult struct {
	Count int `json:"count"`
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	var request deleteRequest
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if err := content.ValidatePath(request.Path, false); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}

	var entry trashEntry
	apiErr := h.mutate(func() *apiError {
		info, err := h.lstatPath(request.Path)
		if err != nil {
			return apiErrorFor(err)
		}

		var items []trashItem
		switch {
		case info.IsDir():
			items = append(items, trashItem{Path: request.Path, Type: "folder", Size: h.directorySize(request.Path)})
			assets, err := h.orphanAssets(
				func(name string) bool {
					return name == request.Path || strings.HasPrefix(name, request.Path+"/")
				},
				func(reference string) bool {
					return reference == request.Path || strings.HasPrefix(reference, request.Path+"/")
				},
			)
			if err != nil {
				return apiErrorFor(err)
			}
			items = append(items, assets...)
		case info.Mode().IsRegular():
			items = append(items, trashItem{Path: request.Path, Type: "file", Size: info.Size()})
			assets, err := h.orphanAssets(
				func(name string) bool { return name == request.Path },
				func(string) bool { return false },
			)
			if err != nil {
				return apiErrorFor(err)
			}
			items = append(items, assets...)
		default:
			return newAPIError(http.StatusBadRequest, "路径无效")
		}

		deletedAt := h.now().UTC()
		id, err := h.newTrashID(deletedAt)
		if err != nil {
			return newAPIError(http.StatusInternalServerError, "无法创建回收站条目")
		}
		if err := h.moveToTrash(id, deletedAt, items); err != nil {
			return apiErrorFor(err)
		}
		entry = trashEntry{ID: id, DeletedAt: deletedAt, Items: items, Size: totalTrashSize(items)}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return entry, nil
}

func (h *Handler) handleTrashList(r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireSession(r); apiErr != nil {
		return nil, apiErr
	}

	h.files.RLock()
	defer h.files.RUnlock()

	entries, err := h.listTrash()
	if err != nil {
		return nil, apiErrorFor(err)
	}
	data := trashData{Entries: entries}
	for _, entry := range entries {
		data.Size += entry.Size
	}
	return data, nil
}

func (h *Handler) handleTrashRestore(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	ids, apiErr := h.decodeTrashIDs(w, r)
	if apiErr != nil {
		return nil, apiErr
	}

	apiErr = h.mutate(func() *apiError {
		entries := make([]trashEntry, 0, len(ids))
		for _, id := range ids {
			entry, err := h.loadTrashEntry(id)
			if err != nil {
				return apiErrorFor(err)
			}
			entries = append(entries, entry)
		}
		for _, entry := range entries {
			for _, item := range entry.Items {
				_, err := h.lstatPath(item.Path)
				if err == nil {
					return newAPIError(http.StatusConflict, "原位置已存在同名内容："+item.Path)
				}
				if !errors.Is(err, fs.ErrNotExist) {
					return newAPIError(http.StatusConflict, "无法还原到原位置："+item.Path)
				}
			}
		}
		for _, entry := range entries {
			if err := h.restoreTrashEntry(entry); err != nil {
				return apiErrorFor(err)
			}
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return trashActionResult{Count: len(ids)}, nil
}

func (h *Handler) handleTrashPurge(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	ids, apiErr := h.decodeTrashIDs(w, r)
	if apiErr != nil {
		return nil, apiErr
	}

	apiErr = h.mutate(func() *apiError {
		for _, id := range ids {
			if err := h.root.RemoveAll(trashEntryDir(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return apiErrorFor(err)
			}
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return trashActionResult{Count: len(ids)}, nil
}

func (h *Handler) handleTrashEmpty(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	count := 0
	apiErr := h.mutate(func() *apiError {
		entries, err := h.listTrash()
		if err != nil {
			return apiErrorFor(err)
		}
		count = len(entries)
		if len(entries) == 0 {
			return nil
		}
		if err := h.root.RemoveAll(trashEntriesDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return apiErrorFor(err)
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return trashActionResult{Count: count}, nil
}

func (h *Handler) decodeTrashIDs(w http.ResponseWriter, r *http.Request) ([]string, *apiError) {
	var request trashIDsRequest
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if len(request.IDs) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "请选择回收站条目")
	}
	seen := make(map[string]bool, len(request.IDs))
	ids := make([]string, 0, len(request.IDs))
	for _, id := range request.IDs {
		if !trashIDPattern.MatchString(id) {
			return nil, newAPIError(http.StatusBadRequest, "回收站条目无效")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// orphanAssets 找出只被待删除内容引用的资源文件。
// skipDocument 判断文档是否属于本次删除，skipReference 判断资源是否已随目录一起移动。
// 任一保留文档无法读取时不回收资源，避免误删仍有引用的素材。
func (h *Handler) orphanAssets(skipDocument, skipReference func(string) bool) ([]trashItem, error) {
	candidates := make(map[string]bool)
	collect := func(name string, data []byte) {
		for _, reference := range content.ResourceReferences(data, name) {
			if skipReference(reference) || !removableAsset(reference) {
				continue
			}
			candidates[reference] = true
		}
	}

	referenced, complete, err := h.documentReferences(skipDocument, collect)
	if err != nil {
		return nil, err
	}
	if !complete || len(candidates) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(candidates))
	for name := range candidates {
		if !referenced[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	items := make([]trashItem, 0, len(names))
	for _, name := range names {
		info, err := h.lstatPath(name)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		items = append(items, trashItem{Path: name, Type: "file", Size: info.Size()})
	}
	return items, nil
}

// documentReferences 遍历文档根目录中的可引用文档，返回其余文档引用的全部路径，
// 并对被 skip 选中的文档调用 onSelected。回收站等隐藏目录不参与统计。
// complete 为 false 表示有文档未能读取，调用方不能据此认定资源无人引用。
func (h *Handler) documentReferences(skip func(string) bool, onSelected func(string, []byte)) (map[string]bool, bool, error) {
	references := make(map[string]bool)
	complete := true
	err := fs.WalkDir(h.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			complete = false
			return nil
		}
		if name == "." {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !referencingDocument(name) {
			return nil
		}
		data, err := h.readReferenceSource(name)
		if err != nil {
			complete = false
			return nil
		}
		if skip(name) {
			if onSelected != nil {
				onSelected(name, data)
			}
			return nil
		}
		for _, reference := range content.ResourceReferences(data, name) {
			references[reference] = true
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return references, complete, nil
}

func referencingDocument(name string) bool {
	switch content.Kind(name) {
	case "markdown", "html":
		return true
	default:
		return false
	}
}

// removableAsset 限定随文档一起回收的资源类型，文档类文件始终作为独立条目处理。
func removableAsset(name string) bool {
	switch content.Kind(name) {
	case "image", "audio", "video":
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".css", ".woff", ".woff2", ".ttf", ".otf":
		return true
	default:
		return false
	}
}

func (h *Handler) readReferenceSource(name string) ([]byte, error) {
	info, err := h.lstatPath(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxReferenceBytes {
		return nil, errNotRegular
	}

	file, err := h.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, maxReferenceBytes))
}

func (h *Handler) directorySize(name string) int64 {
	var total int64
	_ = fs.WalkDir(h.root.FS(), name, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func (h *Handler) listTrash() ([]trashEntry, error) {
	directory, err := h.root.Open(trashEntriesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []trashEntry{}, nil
		}
		return nil, err
	}
	defer directory.Close()

	items, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	entries := make([]trashEntry, 0, len(items))
	for _, item := range items {
		if !item.IsDir() || !trashIDPattern.MatchString(item.Name()) {
			continue
		}
		entry, err := h.loadTrashEntry(item.Name())
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].DeletedAt.Equal(entries[j].DeletedAt) {
			return entries[i].ID > entries[j].ID
		}
		return entries[i].DeletedAt.After(entries[j].DeletedAt)
	})
	return entries, nil
}

func (h *Handler) loadTrashEntry(id string) (trashEntry, error) {
	if !trashIDPattern.MatchString(id) {
		return trashEntry{}, errInvalidName
	}
	data, err := h.root.ReadFile(path.Join(trashEntryDir(id), trashMetaFile))
	if err != nil {
		return trashEntry{}, err
	}
	var meta trashMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return trashEntry{}, errMalformedOrder
	}
	if len(meta.Items) == 0 {
		return trashEntry{}, errMalformedOrder
	}
	return trashEntry{
		ID:        id,
		DeletedAt: meta.DeletedAt,
		Items:     meta.Items,
		Size:      totalTrashSize(meta.Items),
	}, nil
}

func (h *Handler) moveToTrash(id string, deletedAt time.Time, items []trashItem) error {
	moved := make([]trashItem, 0, len(items))
	rollback := func() {
		for index := len(moved) - 1; index >= 0; index-- {
			_ = h.root.Rename(h.trashPayloadPath(id, moved[index].Path), moved[index].Path)
		}
		_ = h.root.RemoveAll(trashEntryDir(id))
	}

	for _, item := range items {
		target := h.trashPayloadPath(id, item.Path)
		if err := h.root.MkdirAll(path.Dir(target), 0o755); err != nil {
			rollback()
			return err
		}
		if err := h.root.Rename(item.Path, target); err != nil {
			rollback()
			return err
		}
		moved = append(moved, item)
	}

	meta := trashMeta{DeletedAt: deletedAt, Items: items}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		rollback()
		return err
	}
	data = append(data, '\n')
	if err := h.root.WriteFile(path.Join(trashEntryDir(id), trashMetaFile), data, 0o644); err != nil {
		rollback()
		return err
	}
	return nil
}

func (h *Handler) restoreTrashEntry(entry trashEntry) error {
	for _, item := range entry.Items {
		source := h.trashPayloadPath(entry.ID, item.Path)
		if parent := path.Dir(item.Path); parent != "." {
			if err := h.root.MkdirAll(parent, 0o755); err != nil {
				return err
			}
		}
		if err := h.root.Rename(source, item.Path); err != nil {
			return err
		}
	}
	if err := h.root.RemoveAll(trashEntryDir(entry.ID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (h *Handler) trashPayloadPath(id, name string) string {
	return path.Join(trashEntryDir(id), trashFilesDir, name)
}

func trashEntryDir(id string) string {
	return path.Join(trashEntriesDir, id)
}

func (h *Handler) newTrashID(deletedAt time.Time) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		token, err := randomToken()
		if err != nil {
			return "", err
		}
		id := deletedAt.Format("20060102T150405Z") + "-" + token[:10]
		if _, err := h.root.Lstat(trashEntryDir(id)); errors.Is(err, fs.ErrNotExist) {
			return id, nil
		}
	}
	return "", errors.New("回收站条目 ID 冲突")
}

func totalTrashSize(items []trashItem) int64 {
	var total int64
	for _, item := range items {
		total += item.Size
	}
	return total
}
