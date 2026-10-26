package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteProtectsReferenceImagesAndDownloads(t *testing.T) {
	for name, reference := range map[string]string{
		"reference": "![image][ref]\n\n[ref]: assets/shared.png\n",
		"download":  "[download](assets/shared.png)",
		"html":      `<a href="assets/shared.png">download</a>`,
	} {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.mustLogin()
			writeDocFile(t, env.docs, "delete.md", "![image](assets/shared.png)")
			writeDocFile(t, env.docs, "keep.md", reference)
			writeDocFile(t, env.docs, "assets/shared.png", "image")
			entry := decodeData[trashEntry](t, env.deletePath("delete.md"))
			if len(entry.Items) != 1 || !exists(env.docs, "assets/shared.png") {
				t.Fatalf("共享素材被误回收: %+v", entry)
			}
			expectStatus(t, env.trashAction("purge", entry.ID), http.StatusOK)
			if !exists(env.docs, "assets/shared.png") {
				t.Fatal("清理回收站误删共享素材")
			}
		})
	}
}

func TestTrashDocumentsKeepSharedAssetsUntilFinalPurge(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "a.md", "![image](assets/shared.png)")
	writeDocFile(t, env.docs, "b.md", "![image](assets/shared.png)")
	writeDocFile(t, env.docs, "assets/shared.png", "image")
	first := decodeData[trashEntry](t, env.deletePath("a.md"))
	second := decodeData[trashEntry](t, env.deletePath("b.md"))
	if !exists(env.docs, "assets/shared.png") {
		t.Fatal("可还原文档的共享素材被移动")
	}
	expectStatus(t, env.trashAction("purge", second.ID), http.StatusOK)
	if !exists(env.docs, "assets/shared.png") {
		t.Fatal("删除另一个条目破坏了可还原文档")
	}
	expectStatus(t, env.trashAction("restore", first.ID), http.StatusOK)
	if !exists(env.docs, "a.md") || !exists(env.docs, "assets/shared.png") {
		t.Fatal("共享素材无法还原使用")
	}
	last := decodeData[trashEntry](t, env.deletePath("a.md"))
	expectStatus(t, env.trashAction("purge", last.ID), http.StatusOK)
	if exists(env.docs, "assets/shared.png") {
		t.Fatal("最后一个引用彻底删除后仍留下素材")
	}
}

func TestDeleteFolderKeepsExternallyReferencedAssets(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "folder/doc.md", "![image](shared.png)")
	writeDocFile(t, env.docs, "folder/shared.png", "shared")
	writeDocFile(t, env.docs, "folder/nested/only.txt", "nested")
	writeDocFile(t, env.docs, "keep.md", "![image](folder/shared.png)")
	entry := decodeData[trashEntry](t, env.deletePath("folder"))
	if exists(env.docs, "folder/doc.md") || exists(env.docs, "folder/nested/only.txt") {
		t.Fatal("待删除文档仍在原处")
	}
	if !exists(env.docs, "folder/shared.png") || len(entry.Items) != 1 || !entry.Items[0].Merge {
		t.Fatalf("目录内共享素材未保留: %+v", entry)
	}
	writeDocFile(t, env.docs, "folder/doc.md", "new")
	expectStatus(t, env.trashAction("restore", entry.ID), http.StatusConflict)
	if exists(env.docs, "folder/nested/only.txt") {
		t.Fatal("冲突还原发生部分移动")
	}
	if readDocFile(t, env.docs, "folder/doc.md") != "new" {
		t.Fatal("还原覆盖了现有文档")
	}
	other := decodeData[trashEntry](t, env.deletePath("folder/doc.md"))
	expectStatus(t, env.trashAction("restore", entry.ID), http.StatusOK)
	if !exists(env.docs, "folder/doc.md") || !exists(env.docs, "folder/nested/only.txt") || !exists(env.docs, "folder/shared.png") {
		t.Fatal("保留目录合并还原失败")
	}
	expectStatus(t, env.trashAction("purge", other.ID), http.StatusOK)
}

func TestCreateRejectsAssetDirectory(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	expectStatus(t, env.createFolder("assets"), http.StatusCreated)
	for _, name := range []string{"assets/note.md", "Assets/note.md", "ASSETS/note.md"} {
		expectStatus(t, env.createDocument(name, "note"), http.StatusBadRequest)
		if exists(env.docs, name) {
			t.Fatalf("素材目录产生了无 ID 的文档: %s", name)
		}
	}
	uploaded := decodeData[uploadData](t, env.upload("image.png", "assets", []byte("image"), true))
	if uploaded.ID != "" || !exists(env.docs, "assets/image.png") {
		t.Fatal("素材上传行为改变")
	}
}

func TestPurgeCountsOnlyExistingEntries(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	result := decodeData[trashActionResult](t, env.trashAction("purge", "20200101T000000Z-abcdef"))
	if result.Count != 0 {
		t.Fatalf("不存在的条目计数 = %d", result.Count)
	}
}

func TestCreateAndUploadRemoveLegacySuffix(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	created := decodeData[documentData](t, env.createDocument("intro_20260101-abcdef.md", "intro"))
	if created.Path != "intro.md" || created.ID == "" || created.ID == "20260101-abcdef" {
		t.Fatalf("新建未使用内部 ID: %+v", created)
	}
	uploaded := decodeData[uploadData](t, env.upload("manual_20260101-ghijkl.pdf", "", []byte("pdf"), true))
	if uploaded.Path != "manual.pdf" || uploaded.ID == "" {
		t.Fatalf("上传未使用干净文件名: %+v", uploaded)
	}
	expectStatus(t, env.upload("manual_20260101-xxxxxx.pdf", "", []byte("must not overwrite"), true), http.StatusConflict)
	if readDocFile(t, env.docs, "manual.pdf") != "pdf" {
		t.Fatal("同名上传覆盖文件")
	}
}

func TestDeleteFolderKeepsCaseInsensitiveReferences(t *testing.T) {
	for _, deleted := range []string{"folder", "FOLDER"} {
		t.Run(deleted, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.mustLogin()
			writeDocFile(t, env.docs, "folder/shared.png", "image")
			writeDocFile(t, env.docs, "folder/doc.md", "doc")
			writeDocFile(t, env.docs, "keep.md", "![image](Folder/SHARED.png)")
			if _, err := os.Stat(filepath.Join(env.docs, "Folder/SHARED.png")); os.IsNotExist(err) {
				t.Skip("文件系统区分大小写")
			} else if err != nil {
				t.Fatal(err)
			}
			entry := decodeData[trashEntry](t, env.deletePath(deleted))
			if len(entry.Items) != 1 || entry.Items[0].Path != "folder" || !entry.Items[0].Merge {
				t.Fatalf("未按实际目录名称保留共享素材: %+v", entry)
			}
			if !exists(env.docs, "folder/shared.png") || exists(env.docs, "folder/doc.md") {
				t.Fatal("大小写不同的引用未被保护")
			}
			expectStatus(t, env.trashAction("restore", entry.ID), http.StatusOK)
			if !exists(env.docs, "folder/doc.md") || readDocFile(t, env.docs, "folder/shared.png") != "image" {
				t.Fatal("大小写别名下还原失败")
			}
		})
	}
}

func TestDeleteFolderDoesNotFoldDistinctPaths(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "folder/shared.png", "delete")
	if exists(env.docs, "Folder/shared.png") {
		t.Skip("文件系统不区分大小写")
	}
	writeDocFile(t, env.docs, "Folder/shared.png", "keep")
	writeDocFile(t, env.docs, "keep.md", "![image](Folder/shared.png)")
	entry := decodeData[trashEntry](t, env.deletePath("folder"))
	if entry.Items[0].Merge || exists(env.docs, "folder/shared.png") {
		t.Fatal("混淆了大小写不同的独立目录")
	}
	if readDocFile(t, env.docs, "Folder/shared.png") != "keep" {
		t.Fatal("修改了另一个目录的素材")
	}
}

func TestDeleteFolderKeepsStylesheetImportAssets(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "folder/theme.css", `@import "base.css" screen;`)
	writeDocFile(t, env.docs, "folder/base.css", `body { background: url(bg.png); }`)
	writeDocFile(t, env.docs, "folder/bg.png", "image")
	writeDocFile(t, env.docs, "folder/doc.md", "doc")
	writeDocFile(t, env.docs, "keep.html", `<link rel="stylesheet" href="folder/theme.css">`)
	entry := decodeData[trashEntry](t, env.deletePath("folder"))
	if len(entry.Items) != 1 || !entry.Items[0].Merge || exists(env.docs, "folder/doc.md") {
		t.Fatalf("目录删除结果无效: %+v", entry)
	}
	expectStatus(t, env.trashAction("purge", entry.ID), http.StatusOK)
	for _, name := range []string{"folder/theme.css", "folder/base.css", "folder/bg.png"} {
		if !exists(env.docs, name) {
			t.Fatalf("CSS 导入依赖被误删: %s", name)
		}
	}
}

func TestStylesheetProtectsSharedResource(t *testing.T) {
	env := newTestEnv(t, nil)
	env.mustLogin()
	writeDocFile(t, env.docs, "delete.md", "![image](assets/shared.png)")
	writeDocFile(t, env.docs, "keep.html", `<link rel="stylesheet" href="assets/theme.css">`)
	writeDocFile(t, env.docs, "assets/theme.css", `body { background: url(shared.png); }`)
	writeDocFile(t, env.docs, "assets/shared.png", "image")
	entry := decodeData[trashEntry](t, env.deletePath("delete.md"))
	if len(entry.Items) != 1 || !exists(env.docs, "assets/shared.png") {
		t.Fatal("样式表引用的共享素材被误回收")
	}
}
