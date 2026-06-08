package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_subtitle_whisper/internal/metadata"
)

func TestNormalizeCreatesIndexesAndDomainFolderValue(t *testing.T) {
	vault := t.TempDir()
	idx := NewObsidianIndex(vault, "领域索引.md", "标签索引.md", 0.82)
	meta := metadata.SummaryMetadata{Domain: "科技", Tags: []string{"OpenAI", "B站"}}
	got, err := idx.Normalize(meta)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Domain != "科技" || len(got.Tags) != 2 {
		t.Fatalf("normalized = %+v", got)
	}
	domainIndex := readFile(t, filepath.Join(vault, "领域索引.md"))
	tagIndex := readFile(t, filepath.Join(vault, "标签索引.md"))
	if !strings.Contains(domainIndex, "- 科技") {
		t.Fatalf("domain index missing value: %s", domainIndex)
	}
	if !strings.Contains(tagIndex, "- OpenAI") || !strings.Contains(tagIndex, "- B站") {
		t.Fatalf("tag index missing values: %s", tagIndex)
	}
}

func TestNormalizeReusesSimilarDomain(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "领域索引.md"), []byte("# 领域索引\n\n- 人工智能\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := NewObsidianIndex(vault, "领域索引.md", "标签索引.md", 0.5)
	got, err := idx.Normalize(metadata.SummaryMetadata{Domain: "AI人工智能"})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Domain != "人工智能" {
		t.Fatalf("expected existing domain, got %q", got.Domain)
	}
}

func TestNormalizeDefaultsEmptyDomain(t *testing.T) {
	vault := t.TempDir()
	idx := NewObsidianIndex(vault, "领域索引.md", "标签索引.md", 0.82)
	got, err := idx.Normalize(metadata.SummaryMetadata{})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Domain != "未分类" {
		t.Fatalf("expected fallback domain, got %q", got.Domain)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
