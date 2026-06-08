package knowledge

import (
	"path/filepath"
	"strings"
	"testing"

	"go_subtitle_whisper/internal/metadata"
)

func TestNormalizeCleansDomainAndTagPunctuation(t *testing.T) {
	vault := t.TempDir()
	idx := NewObsidianIndex(vault, "domains.md", "tags.md", 0.82)

	got, err := idx.Normalize(metadata.SummaryMetadata{
		Domain: "`\u4eba\u5de5\u667a\u80fd`:",
		Tags:   []string{"`OpenAI`", "#\u89c6\u9891\uff0c"},
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Domain != "\u4eba\u5de5\u667a\u80fd" {
		t.Fatalf("domain = %q, want clean domain", got.Domain)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "OpenAI" || got.Tags[1] != "\u89c6\u9891" {
		t.Fatalf("tags = %#v, want clean tags", got.Tags)
	}

	domainIndex := readFile(t, filepath.Join(vault, "domains.md"))
	tagIndex := readFile(t, filepath.Join(vault, "tags.md"))
	if strings.Contains(domainIndex, "`") || strings.Contains(tagIndex, "`") {
		t.Fatalf("indexes should not contain backticks:\n%s\n%s", domainIndex, tagIndex)
	}
}
