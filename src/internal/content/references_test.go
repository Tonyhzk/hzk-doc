package content

import (
	"strings"
	"testing"
)

func TestResourceReferencesMarkdown(t *testing.T) {
	source := []byte("# 标题\n\n" +
		"![本地](assets/pic.png)\n" +
		"![上级](../assets/other.png)\n" +
		"![远程](https://example.com/remote.png)\n" +
		"![绝对](/shared/logo.png)\n" +
		"[文档链接](guide.md)\n")

	got := ResourceReferences(source, "notes/page.md")
	want := []string{"assets/other.png", "assets/pic.png", "shared/logo.png"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestResourceReferencesHTML(t *testing.T) {
	source := []byte(`<link rel="stylesheet" href="../assets/site.css">` +
		`<img src="img/hero.png" srcset="img/a.png 1x, assets/b.png 2x">` +
		`<video poster="poster.jpg"><source src="media/clip.mp4"></video>` +
		`<a href="other.md">链接</a>` +
		`<div style="background:url('bg.png')"></div>` +
		`<style>.x{background:url(theme.png)}</style>`)

	got := ResourceReferences(source, "pages/index.html")
	want := []string{
		"assets/b.png",
		"assets/site.css",
		"pages/bg.png",
		"pages/img/a.png",
		"pages/img/hero.png",
		"pages/media/clip.mp4",
		"pages/poster.jpg",
		"pages/theme.png",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestResourceReferencesIgnoresUnsafe(t *testing.T) {
	source := []byte("![a](../../escape.png)\n<img src=\"javascript:alert(1)\">\n<img src=\".hidden.png\">")
	if got := ResourceReferences(source, "doc.md"); len(got) != 0 {
		t.Fatalf("references = %v, want none", got)
	}
}
