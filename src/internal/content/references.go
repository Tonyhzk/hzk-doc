package content

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

// TrashDir 保存被删除的文档与资源，位于文档根目录内，不出现在公开目录中。
const TrashDir = ".hzk-doc-trash"

var (
	markdownImagePattern = regexp.MustCompile(`!\[[^\]\n]*\]\(\s*(?:<([^>\n]*)>|([^)\s]+))`)
	htmlResourceTag      = regexp.MustCompile(`(?is)<(img|video|audio|source|track|link|embed|object)\b([^>]*)>`)
	htmlAttribute        = regexp.MustCompile(`(?is)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	styleAttribute       = regexp.MustCompile(`(?is)\bstyle\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	styleBlock           = regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)
	cssURL               = regexp.MustCompile(`(?i)url\(\s*(?:'([^']*)'|"([^"]*)"|([^)'"\s]+))\s*\)`)
)

// ResourceReferences 返回文档以资源形式引用的站内文件，路径相对文档根目录。
// 只识别图片、媒体、样式等资源位置，普通链接指向的文档不计入。
func ResourceReferences(source []byte, docPath string) []string {
	docDir := path.Dir(docPath)
	if docDir == "." {
		docDir = ""
	}

	seen := make(map[string]bool)
	collect := func(raw string) {
		resolved, ok := resolveReference(raw, docDir)
		if ok {
			seen[resolved] = true
		}
	}

	text := string(source)
	for _, match := range markdownImagePattern.FindAllStringSubmatch(text, -1) {
		collect(match[1])
		collect(match[2])
	}
	for _, match := range htmlResourceTag.FindAllStringSubmatch(text, -1) {
		tag := strings.ToLower(match[1])
		for _, attribute := range htmlAttribute.FindAllStringSubmatch(match[2], -1) {
			name := strings.ToLower(attribute[1])
			value := attribute[2]
			if value == "" {
				value = attribute[3]
			}
			if value == "" {
				value = attribute[4]
			}
			switch name {
			case "src", "poster", "data-src", "xlink:href":
				collect(value)
			case "href":
				if tag == "link" {
					collect(value)
				}
			case "srcset":
				for _, candidate := range strings.Split(value, ",") {
					fields := strings.Fields(candidate)
					if len(fields) > 0 {
						collect(fields[0])
					}
				}
			}
		}
	}
	for _, match := range styleAttribute.FindAllStringSubmatch(text, -1) {
		collectCSSURLs(match[1], collect)
		collectCSSURLs(match[2], collect)
	}
	for _, match := range styleBlock.FindAllStringSubmatch(text, -1) {
		collectCSSURLs(match[1], collect)
	}

	references := make([]string, 0, len(seen))
	for reference := range seen {
		references = append(references, reference)
	}
	sort.Strings(references)
	return references
}

func collectCSSURLs(css string, collect func(string)) {
	for _, match := range cssURL.FindAllStringSubmatch(css, -1) {
		collect(match[1])
		collect(match[2])
		collect(match[3])
	}
}

// resolveReference 把文档中的引用解析为文档根目录下的相对路径，规则与阅读端一致。
func resolveReference(raw, docDir string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\\\x00") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return "", false
	}
	reference := parsed.Path
	if reference == "" {
		return "", false
	}
	switch {
	case strings.HasPrefix(reference, "/"):
		reference = strings.TrimPrefix(reference, "/")
	case reference == "assets" || strings.HasPrefix(reference, "assets/"):
	default:
		reference = path.Join(docDir, reference)
	}
	reference = path.Clean(reference)
	if reference == "." || reference == ".." || strings.HasPrefix(reference, "../") {
		return "", false
	}
	for _, segment := range strings.Split(reference, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return "", false
		}
	}
	return reference, true
}
