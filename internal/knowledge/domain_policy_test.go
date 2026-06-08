package knowledge

import (
	"path/filepath"
	"strings"
	"testing"

	"go_subtitle_whisper/internal/metadata"
)

func TestNormalizeUsesBroadDomainAndWritesObsidianLinks(t *testing.T) {
	vault := t.TempDir()
	idx := NewObsidianIndex(vault, "domains.md", "tags.md", 0.82)

	got, err := idx.Normalize(metadata.SummaryMetadata{
		Domain: "\u91cf\u5316\u7ecf\u6d4e",
		Tags:   []string{"#\u7ecf\u6d4e\u5206\u6790\uff0c", "`\u5b8f\u89c2`"},
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Domain != "\u7ecf\u6d4e" {
		t.Fatalf("domain = %q, want broad economic domain", got.Domain)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "\u7ecf\u6d4e\u5206\u6790" || got.Tags[1] != "\u5b8f\u89c2" {
		t.Fatalf("tags = %#v, want clean tags", got.Tags)
	}

	domainIndex := readFile(t, filepath.Join(vault, "domains.md"))
	tagIndex := readFile(t, filepath.Join(vault, "tags.md"))
	if !strings.Contains(domainIndex, "- [[\u7ecf\u6d4e]]") {
		t.Fatalf("domain index should contain obsidian link, got:\n%s", domainIndex)
	}
	if !strings.Contains(tagIndex, "- [[\u7ecf\u6d4e\u5206\u6790]]") || !strings.Contains(tagIndex, "- [[\u5b8f\u89c2]]") {
		t.Fatalf("tag index should contain obsidian links, got:\n%s", tagIndex)
	}
}

func TestNormalizeMapsLearningDomainsToArtificialIntelligence(t *testing.T) {
	vault := t.TempDir()
	idx := NewObsidianIndex(vault, "domains.md", "tags.md", 0.82)

	for _, input := range []string{
		"\u6df1\u5ea6\u5b66\u4e60",
		"\u673a\u5668\u5b66\u4e60",
		"\u5f3a\u5316\u5b66\u4e60",
	} {
		got, err := idx.Normalize(metadata.SummaryMetadata{Domain: input})
		if err != nil {
			t.Fatalf("Normalize(%q) error = %v", input, err)
		}
		if got.Domain != "\u4eba\u5de5\u667a\u80fd" {
			t.Fatalf("domain for %q = %q, want artificial intelligence", input, got.Domain)
		}
	}
}
