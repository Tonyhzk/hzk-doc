package admin

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"

	"hzk-doc/internal/catalog"
	"hzk-doc/internal/content"
)

const maxReferenceBytes = 8 << 20

var trashIDPattern = regexp.MustCompile(`^(?:[a-f0-9]{32}|[0-9]{8}T[0-9]{6}Z-[A-Za-z0-9_-]{6,32})$`)

type trashItem = catalog.Item
type trashEntry = catalog.Trash

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
		canonical, err := h.canonicalPath(request.Path)
		if err != nil {
			return apiErrorFor(err)
		}
		request.Path = canonical
		info, err := h.lstatPath(request.Path)
		if err != nil {
			return apiErrorFor(err)
		}
		var items []trashItem
		var retained map[string]bool
		switch {
		case info.IsDir():
			retained, err = h.retainedFolderAssets(request.Path)
			if err != nil {
				return apiErrorFor(err)
			}
			size := h.directorySize(request.Path)
			for name := range retained {
				if file, err := h.lstatPath(name); err == nil {
					size -= file.Size()
				}
			}
			items = append(items, trashItem{Path: request.Path, Type: "folder", Size: size, Merge: len(retained) > 0})
		case info.Mode().IsRegular():
			items = append(items, trashItem{Path: request.Path, Type: "file", Size: info.Size()})
		default:
			return newAPIError(http.StatusBadRequest, "路径无效")
		}
		selected := func(name string) bool {
			return name == request.Path || (info.IsDir() && strings.HasPrefix(name, request.Path+"/"))
		}
		assets, err := h.orphanAssets(func(name string) bool { return selected(name) && !retained[name] }, selected)
		if err != nil {
			return apiErrorFor(err)
		}
		items = append(items, assets...)
		entry = trashEntry{ID: catalog.NewID(), DeletedAt: h.now().UTC(), Items: items}
		for _, item := range items {
			entry.Size += item.Size
		}
		return errorOrNil(h.catalog.Delete(entry, retained))
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return entry, nil
}

func errorOrNil(err error) *apiError {
	if err != nil {
		return apiErrorFor(err)
	}
	return nil
}

func (h *Handler) handleTrashList(r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireSession(r); apiErr != nil {
		return nil, apiErr
	}
	h.files.RLock()
	defer h.files.RUnlock()
	entries, err := h.catalog.TrashList()
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
			entry, err := h.catalog.GetTrash(id)
			if err != nil {
				return apiErrorFor(err)
			}
			entries = append(entries, entry)
		}
		return errorOrNil(h.catalog.Restore(entries))
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
	count := 0
	apiErr = h.mutate(func() *apiError {
		var entries []trashEntry
		for _, id := range ids {
			entry, err := h.catalog.GetTrash(id)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return apiErrorFor(err)
			}
			entries = append(entries, entry)
		}
		extras, err := h.purgeAssets(entries)
		if err != nil {
			return apiErrorFor(err)
		}
		if err := h.catalog.Purge(entries, extras...); err != nil {
			return apiErrorFor(err)
		}
		count = len(entries)
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return trashActionResult{Count: count}, nil
}

func (h *Handler) handleTrashEmpty(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}
	count := 0
	apiErr := h.mutate(func() *apiError {
		entries, err := h.catalog.TrashList()
		if err != nil {
			return apiErrorFor(err)
		}
		extras, err := h.purgeAssets(entries)
		if err != nil {
			return apiErrorFor(err)
		}
		if err := h.catalog.Purge(entries, extras...); err != nil {
			return apiErrorFor(err)
		}
		count = len(entries)
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
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// 任一保留文档无法读取时不回收资源，避免误删仍有引用的素材。
func (h *Handler) orphanAssets(skipDocument, skipReference func(string) bool) ([]trashItem, error) {
	candidates := make(map[string]bool)
	collect := func(name string, data []byte) {
		for _, reference := range h.resourceReferences(data, name) {
			if !skipReference(reference) && removableAsset(reference) {
				candidates[reference] = true
			}
		}
	}
	referenced, complete, err := h.documentReferences(skipDocument, collect)
	if err != nil || !complete || len(candidates) == 0 {
		return nil, err
	}
	trashed, trashComplete, err := h.trashReferences(nil, nil)
	if err != nil || !trashComplete {
		return nil, err
	}
	for name := range trashed {
		referenced[name] = true
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
		if err == nil && info.Mode().IsRegular() {
			items = append(items, trashItem{Path: name, Type: "file", Size: info.Size()})
		}
	}
	return items, nil
}

func (h *Handler) resourceReferences(data []byte, name string) []string {
	refs := content.ResourceReferences(data, name)
	for i, ref := range refs {
		if doc, err := h.catalog.FindPath(ref); err == nil {
			ref = doc.Path
		}
		if canonical, err := h.canonicalPath(ref); err == nil {
			ref = canonical
		}
		refs[i] = ref
	}
	return refs
}

// complete 为 false 表示有文档未能读取，不能据此认定资源无人引用。
func (h *Handler) documentReferences(skip func(string) bool, onSelected func(string, []byte)) (map[string]bool, bool, error) {
	references := make(map[string]bool)
	complete := true
	err := fs.WalkDir(h.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			complete = false
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
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
		for _, reference := range h.resourceReferences(data, name) {
			references[reference] = true
		}
		return nil
	})
	return references, complete, err
}

func referencingDocument(name string) bool {
	if strings.EqualFold(path.Ext(name), ".css") {
		return true
	}
	switch content.Kind(name) {
	case "markdown", "html":
		return true
	default:
		return false
	}
}

// 文档类文件始终作为独立条目处理。
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
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
