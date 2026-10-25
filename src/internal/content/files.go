package content

import (
	"errors"
	"io/fs"
	"path"
	"strings"
)

const OrderFile = ".hzk-doc-order.json"

func Kind(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mark":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico", ".avif":
		return "image"
	case ".docx":
		return "docx"
	case ".xlsx":
		return "xlsx"
	case ".pptx":
		return "pptx"
	case ".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a", ".oga":
		return "audio"
	case ".mp4", ".webm", ".mov", ".m4v", ".ogv":
		return "video"
	case ".txt":
		return "text"
	default:
		return ""
	}
}

func AllowedFile(name string) bool {
	if Kind(name) != "" {
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".css", ".woff", ".woff2", ".ttf", ".otf":
		return true
	default:
		return false
	}
}

func Editable(name string) bool {
	switch Kind(name) {
	case "markdown", "html", "text":
		return true
	default:
		return false
	}
}

func ValidatePath(name string, allowRoot bool) error {
	if name == "" && allowRoot {
		return nil
	}
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\\x00:") {
		return errors.New("无效的文档路径")
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.TrimSpace(part) != part {
			return errors.New("不能访问隐藏文件或含首尾空格的路径")
		}
	}
	return nil
}
