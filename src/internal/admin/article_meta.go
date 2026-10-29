package admin

import (
	"net/http"

	"hzk-doc/internal/catalog"
)

type articleMetaData struct {
	ShowFileType *bool `json:"showFileType"`
	ShowFileSize *bool `json:"showFileSize"`
}

func (h *Handler) handleArticleMeta(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}
	var request articleMetaData
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if request.ShowFileType == nil || request.ShowFileSize == nil {
		return nil, newAPIError(http.StatusBadRequest, "文章信息设置必须包含两个布尔值")
	}
	meta := catalog.ArticleMeta{ShowFileType: *request.ShowFileType, ShowFileSize: *request.ShowFileSize}

	h.files.RLock()
	defer h.files.RUnlock()
	h.catalog.Lock()
	err := h.catalog.SetArticleMeta(meta)
	h.catalog.Unlock()
	if err != nil {
		return nil, apiErrorFor(err)
	}
	return meta, nil
}
