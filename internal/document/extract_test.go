package document

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractDOCX(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.docx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<?xml version="1.0"?><w:document xmlns:w="w"><w:body><w:p><w:r><w:t>产品目标：故障恢复时间降低30%</w:t></w:r></w:p><w:p><w:r><w:t>风险：Agent不得直接发布生产</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	_ = os.WriteFile(path, buf.Bytes(), 0o600)
	blocks, digest, media, err := ExtractFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if digest == "" || media == "" {
		t.Fatal("digest/media empty")
	}
	if blocks[0].Locator != "docx:paragraph:1" {
		t.Fatalf("unexpected locator %s", blocks[0].Locator)
	}
}

func TestExtractPPTX(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pptx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w2, _ := zw.Create("ppt/slides/slide2.xml")
	_, _ = w2.Write([]byte(`<?xml version="1.0"?><p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>第二页：业务结果指标</a:t></a:r></a:p></p:sld>`))
	w1, _ := zw.Create("ppt/slides/slide1.xml")
	_, _ = w1.Write([]byte(`<?xml version="1.0"?><p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>第一页：产品需求</a:t></a:r></a:p></p:sld>`))
	_ = zw.Close()
	_ = os.WriteFile(path, buf.Bytes(), 0o600)
	blocks, _, _, err := ExtractFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want 2, got %d", len(blocks))
	}
	if blocks[0].Locator != "pptx:slide:1" || blocks[1].Locator != "pptx:slide:2" {
		t.Fatalf("slides not sorted: %+v", blocks)
	}
}
