package admin

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"hzk-doc/internal/content"
)

type sourceData struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

type documentRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

type documentData struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

type folderRequest struct {
	Path string `json:"path"`
}

type folderData struct {
	Path string `json:"path"`
}

type uploadData struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (h *Handler) handleSource(r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireSession(r); apiErr != nil {
		return nil, apiErr
	}

	name := r.URL.Query().Get("path")
	if err := validEditablePath(name, false); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}

	h.files.RLock()
	defer h.files.RUnlock()

	data, _, err := h.readRegularFile(name)
	if err != nil {
		return nil, apiErrorFor(err)
	}
	return sourceData{Path: name, Content: string(data), Revision: contentRevision(data)}, nil
}

func (h *Handler) handleCreateDocument(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	var request documentRequest
	if apiErr := h.decodeJSON(w, r, maxDocumentBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if err := validEditablePath(request.Path, false); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}

	apiErr := h.mutate(func() *apiError {
		if err := h.ensureParentDir(request.Path); err != nil {
			return apiErrorFor(err)
		}
		if err := h.writeNewFile(request.Path, []byte(request.Content)); err != nil {
			return apiErrorFor(err)
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return createdResult{data: documentData{Path: request.Path, Revision: contentRevision([]byte(request.Content))}}, nil
}

func (h *Handler) handleEditDocument(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	var request documentRequest
	if apiErr := h.decodeJSON(w, r, maxDocumentBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if err := validEditablePath(request.Path, false); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}

	apiErr := h.mutate(func() *apiError {
		current, info, err := h.readRegularFile(request.Path)
		if err != nil {
			return apiErrorFor(err)
		}
		if contentRevision(current) != request.Revision {
			return newAPIError(http.StatusConflict, "文件已被修改")
		}
		perm := info.Mode().Perm()
		if perm == 0 {
			perm = 0o644
		}
		if err := h.writeFileAtomic(request.Path, []byte(request.Content), perm); err != nil {
			return apiErrorFor(err)
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return documentData{Path: request.Path, Revision: contentRevision([]byte(request.Content))}, nil
}

func (h *Handler) handleCreateFolder(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	var request folderRequest
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if err := content.ValidatePath(request.Path, false); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}

	apiErr := h.mutate(func() *apiError {
		if err := h.ensureParentDir(request.Path); err != nil {
			return apiErrorFor(err)
		}
		if err := h.root.Mkdir(request.Path, 0o755); err != nil {
			return apiErrorFor(err)
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return createdResult{data: folderData{Path: request.Path}}, nil
}

func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return nil, newAPIError(http.StatusUnsupportedMediaType, "请求必须是 multipart/form-data")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+maxMultipartOverhead)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, newAPIError(http.StatusBadRequest, "请求格式无效")
	}

	var (
		data         []byte
		filename     string
		directory    string
		hasFile      bool
		hasDirectory bool
	)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if isBodyTooLarge(err) {
				return nil, newAPIError(http.StatusRequestEntityTooLarge, "文件超过大小限制")
			}
			return nil, newAPIError(http.StatusBadRequest, "请求格式无效")
		}

		switch part.FormName() {
		case "file":
			if hasFile {
				part.Close()
				return nil, newAPIError(http.StatusBadRequest, "只允许上传一个文件")
			}
			hasFile = true
			rawName, nameErr := rawPartFilename(part)
			if nameErr != nil {
				part.Close()
				return nil, newAPIError(http.StatusBadRequest, "文件名无效")
			}
			payload, readErr := io.ReadAll(io.LimitReader(part, maxUploadBytes+1))
			part.Close()
			if readErr != nil {
				if isBodyTooLarge(readErr) {
					return nil, newAPIError(http.StatusRequestEntityTooLarge, "文件超过大小限制")
				}
				return nil, newAPIError(http.StatusBadRequest, "请求格式无效")
			}
			if int64(len(payload)) > maxUploadBytes {
				return nil, newAPIError(http.StatusRequestEntityTooLarge, "文件超过大小限制")
			}
			filename = rawName
			data = payload
		case "directory":
			if hasDirectory {
				part.Close()
				return nil, newAPIError(http.StatusBadRequest, "请求格式无效")
			}
			hasDirectory = true
			value, readErr := io.ReadAll(io.LimitReader(part, maxLoginBody+1))
			part.Close()
			if readErr != nil {
				if isBodyTooLarge(readErr) {
					return nil, newAPIError(http.StatusRequestEntityTooLarge, "请求内容过大")
				}
				return nil, newAPIError(http.StatusBadRequest, "请求格式无效")
			}
			if len(value) > maxLoginBody {
				return nil, newAPIError(http.StatusBadRequest, "请求格式无效")
			}
			directory = string(value)
		default:
			part.Close()
		}
	}

	if !hasFile || filename == "" {
		return nil, newAPIError(http.StatusBadRequest, "缺少上传文件")
	}
	if !hasDirectory {
		return nil, newAPIError(http.StatusBadRequest, "缺少目标目录")
	}
	if err := content.ValidatePath(directory, true); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "目标目录无效")
	}
	if err := validateUploadName(filename); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "文件名无效")
	}
	target := joinRelative(directory, filename)
	if err := content.ValidatePath(target, false); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "路径无效")
	}
	if !content.AllowedFile(filename) {
		return nil, newAPIError(http.StatusBadRequest, "不支持的文件类型")
	}

	apiErr := h.mutate(func() *apiError {
		dirHandle, err := h.openDir(directory)
		if err != nil {
			return apiErrorFor(err)
		}
		dirHandle.Close()

		if err := h.writeNewFile(target, data); err != nil {
			return apiErrorFor(err)
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return createdResult{data: uploadData{Path: target, Size: int64(len(data))}}, nil
}

func validateUploadName(name string) error {
	if name == "" || name == "." || name == ".." {
		return errInvalidName
	}
	if strings.HasPrefix(name, ".") {
		return errInvalidName
	}
	if strings.ContainsAny(name, "/\\\x00:") {
		return errInvalidName
	}
	if path.Base(name) != name {
		return errInvalidName
	}
	if strings.TrimSpace(name) != name {
		return errInvalidName
	}
	return nil
}

func rawPartFilename(part *multipart.Part) (string, error) {
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil {
		return "", errInvalidName
	}
	name := params["filename"]
	if name == "" {
		return "", errInvalidName
	}
	return name, nil
}

func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
