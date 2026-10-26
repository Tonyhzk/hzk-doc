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
		"![参考][ref]\n" +
		"![带空格](<assets/sp ace.png> \"标题\")\n" +
		"[文档链接](guide.md)\n" +
		"[下载](assets/manual.pdf)\n\n" +
		"[ref]: assets/reference.png \"说明\"\n\n" +
		"```\n![代码示例](assets/code.png)\n```\n")

	got := ResourceReferences(source, "notes/page.md")
	want := []string{
		"assets/manual.pdf",
		"assets/other.png",
		"assets/pic.png",
		"assets/reference.png",
		"assets/sp ace.png",
		"notes/guide.md",
		"shared/logo.png",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestResourceReferencesHTML(t *testing.T) {
	source := []byte(`<link rel="stylesheet" href="../assets/site.css">` +
		`<img src="img/hero.png" srcset="img/a.png 1x, assets/b.png 2x">` +
		`<video poster="poster.jpg"><source src="media/clip.mp4"></video>` +
		`<a href="other.md">链接</a>` +
		`<a href="manual.zip">下载</a>` +
		`<div style="background:url('bg.png')"></div>` +
		`<style>.x{background:url(theme.png)}</style>` +
		`<img src="img/a&amp;b.png">` +
		`<img src="data:image/png;base64,AAAA" srcset="data:image/png;base64,AAA 1x, img/data.png 2x">` +
		`<a href="#section">锚点</a>`)

	got := ResourceReferences(source, "pages/index.html")
	want := []string{
		"assets/b.png",
		"assets/site.css",
		"pages/bg.png",
		"pages/img/a&b.png",
		"pages/img/a.png",
		"pages/img/data.png",
		"pages/img/hero.png",
		"pages/manual.zip",
		"pages/media/clip.mp4",
		"pages/other.md",
		"pages/poster.jpg",
		"pages/theme.png",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestResourceReferencesStyleElementImports(t *testing.T) {
	source := []byte(`<style>
@import "theme/base.css";
@import 'print.css' print and (min-width: 600px);
@import "https://example.com/remote.css";
@import "../../escape.css";
.hero { background: url(../assets/bg.png); }
</style>
<div style="@import 'attr.css'; background:url(inline.png)"></div>`)

	got := ResourceReferences(source, "pages/index.html")
	want := []string{
		"assets/bg.png",
		"pages/inline.png",
		"pages/print.css",
		"pages/theme/base.css",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestResourceReferencesStyleSheet(t *testing.T) {
	source := []byte(`@import url("base.css");
@import "layout.css";
@import 'print.css' print and (min-width: 600px);
@import "https://example.com/remote.css" screen;
@import "../../escape.css";
@import ".hidden.css";
.hero { background: url('../assets/bg.jpg') no-repeat; }
.icon { background-image: url( "img/icon.svg" ); }
@font-face { src: url(fonts/text.woff2?v=2) format("woff2"); }
.remote { background: url(https://example.com/a.png); }
.inline { background: url(data:image/png;base64,AAAA); }
.root { background: url("/shared/logo.png"); }
.note::after { content: "<img src='theme/fake.png'>"; }
`)

	got := ResourceReferences(source, "theme/site.css")
	want := []string{
		"assets/bg.jpg",
		"shared/logo.png",
		"theme/base.css",
		"theme/fonts/text.woff2",
		"theme/img/icon.svg",
		"theme/layout.css",
		"theme/print.css",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestResourceReferencesIgnoresUnsafe(t *testing.T) {
	source := []byte("![a](../../escape.png)\n" +
		"![b](.hidden.png)\n" +
		"![c](//example.com/x.png)\n" +
		"![d](data:image/png;base64,AAAA)\n" +
		"![e](//cdn.example.com/y.png)\n" +
		"<img src=\"javascript:alert(1)\">\n" +
		"<style>.x{background:url(data:image/gif;base64,AAAA)}</style>\n" +
		"[f](../up.pdf)\n")
	if got := ResourceReferences(source, "doc.md"); len(got) != 0 {
		t.Fatalf("references = %v, want none", got)
	}
}

func TestResourceReferencesMarkdownUnusedDefinition(t *testing.T) {
	source := []byte("![未定义][missing]\n\n[unused]: assets/unused.png\n")
	if got := ResourceReferences(source, "doc.md"); len(got) != 0 {
		t.Fatalf("references = %v, want none", got)
	}
}
