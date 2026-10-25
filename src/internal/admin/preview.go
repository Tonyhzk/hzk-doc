package admin

import (
	"net/http"
)

type previewRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type previewData struct {
	Content string `json:"content"`
}

func (h *Handler) handlePreview(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	var request previewRequest
	if apiErr := h.decodeJSON(w, r, maxDocumentBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if err := validEditablePath(request.Path, false); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}
	if h.preview == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, "预览功能不可用")
	}

	rendered, err := h.preview(request.Path, request.Content)
	if err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "预览失败")
	}
	return previewData{Content: rendered}, nil
}
