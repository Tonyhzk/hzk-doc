package admin

import (
	"errors"
	"net/http"
	"path"
	"strings"

	"hzk-doc/internal/catalog"
	"hzk-doc/internal/content"
)

type renameRequest struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type renameData struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func (h *Handler) documentID(name string) string {
	if !catalog.IsDocument(name) {
		return ""
	}
	if doc, err := h.catalog.FindPath(name); err == nil {
		return doc.ID
	}
	return ""
}

func (h *Handler) createFile(name string, data []byte) error {
	if err := h.writeNewFile(name, data); err != nil {
		return err
	}
	if catalog.IsDocument(name) {
		if err := h.catalog.Register([]string{name}); err != nil {
			return errors.Join(err, h.root.Remove(name))
		}
	}
	return nil
}

func (h *Handler) handleRename(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}
	var request renameRequest
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if !catalog.IsDocument(request.Path) || validateUploadName(request.Name) != nil {
		return nil, newAPIError(http.StatusBadRequest, "文档路径或名称无效")
	}
	request.Name = catalog.CleanName(request.Name)
	target := path.Join(path.Dir(request.Path), request.Name)
	if content.ValidatePath(target, false) != nil || !strings.EqualFold(path.Ext(target), path.Ext(request.Path)) {
		return nil, newAPIError(http.StatusBadRequest, "请保留原文件扩展名")
	}
	var result renameData
	apiErr := h.mutate(func() *apiError {
		info, err := h.lstatPath(request.Path)
		if err != nil {
			return apiErrorFor(err)
		}
		if !info.Mode().IsRegular() {
			return newAPIError(http.StatusBadRequest, "只能修改文档文件名")
		}
		doc, err := h.catalog.FindPath(request.Path)
		if err != nil {
			return apiErrorFor(err)
		}
		if target != request.Path {
			if err := h.catalog.Rename(request.Path, target); err != nil {
				return apiErrorFor(err)
			}
		}
		result = renameData{ID: doc.ID, Path: target}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return result, nil
}
