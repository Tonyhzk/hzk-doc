package content

import "testing"

func TestValidatePath(t *testing.T) {
	valid := []string{"开始使用.md", "指南/含 空格.MD", "assets/image.svg", "资料/2026-说明.docx"}
	for _, name := range valid {
		if err := ValidatePath(name, false); err != nil {
			t.Errorf("有效路径 %q 被拒绝: %v", name, err)
		}
	}
	invalid := []string{"", ".", "..", "../secret.md", "/absolute.md", "a/../b.md", "a//b.md", "a\\b.md", "a/", ".env", "a/.secret.md", "a/ x.md", "x.md ", "C:/a.md", "a\x00.md"}
	for _, name := range invalid {
		if err := ValidatePath(name, false); err == nil {
			t.Errorf("不安全路径 %q 未被拒绝", name)
		}
	}
	if err := ValidatePath("", true); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePath(".", true); err == nil {
		t.Fatal("根目录的唯一外部表示应为空字符串")
	}
}

func TestFilePolicies(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		allowed  bool
		editable bool
	}{
		{"简介.MD", "markdown", true, true},
		{"说明.markdown", "markdown", true, true},
		{"index.htm", "html", true, true},
		{"说明.txt", "text", true, true},
		{"report.pdf", "pdf", true, false},
		{"report.docx", "docx", true, false},
		{"report.xlsx", "xlsx", true, false},
		{"report.pptx", "pptx", true, false},
		{"图.svg", "image", true, false},
		{"clip.mp4", "video", true, false},
		{"sound.mp3", "audio", true, false},
		{"style.css", "", true, false},
		{"font.woff2", "", true, false},
		{"script.js", "", false, false},
		{"private.key", "", false, false},
		{"config.json", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if Kind(tc.name) != tc.kind || AllowedFile(tc.name) != tc.allowed || Editable(tc.name) != tc.editable {
				t.Fatalf("文件策略错误: kind=%q allowed=%v editable=%v", Kind(tc.name), AllowedFile(tc.name), Editable(tc.name))
			}
		})
	}
}
