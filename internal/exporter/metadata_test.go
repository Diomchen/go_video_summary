package exporter

import (
	"strings"
	"testing"

	"go_subtitle_whisper/internal/domain"
)

func TestIMAMetadataPrefixPrefersNewMetadata(t *testing.T) {
	task := &domain.Task{
		AuthorName: "old-up",
		SourceURL:  "https://old.example.com",
		DomainTags: []string{"old-domain"},
		UPName:     "new-up",
		SourceLink: "https://new.example.com",
		Domain:     "科技",
		Tags:       []string{"OpenAI", "B站"},
	}

	got := buildIMAMetadataPrefix(task)
	for _, want := range []string{
		"**UP主：** new-up",
		"**视频链接：** [https://new.example.com](https://new.example.com)",
		"**领域：** 科技",
		"**标签：** OpenAI | B站",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected IMA metadata to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old-up") || strings.Contains(got, "old-domain") {
		t.Fatalf("expected new metadata to take precedence, got:\n%s", got)
	}
}

func TestNotionMetadataBlocksPreferNewMetadata(t *testing.T) {
	task := &domain.Task{
		AuthorName: "old-up",
		SourceURL:  "https://old.example.com",
		DomainTags: []string{"old-domain"},
		UPName:     "new-up",
		SourceLink: "https://new.example.com",
		Domain:     "科技",
		Tags:       []string{"OpenAI", "B站"},
	}

	blocks := buildNotionMetadataBlocks(task)
	text := notionBlocksText(blocks)
	for _, want := range []string{
		"UP主：new-up",
		"视频链接：https://new.example.com",
		"领域：科技",
		"标签：OpenAI | B站",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected Notion metadata to contain %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "old-up") || strings.Contains(text, "old-domain") {
		t.Fatalf("expected new metadata to take precedence, got:\n%s", text)
	}
}

func notionBlocksText(blocks []map[string]any) string {
	var out strings.Builder
	for _, block := range blocks {
		paragraph, ok := block["paragraph"].(map[string]any)
		if !ok {
			continue
		}
		richText, ok := paragraph["rich_text"].([]map[string]any)
		if !ok {
			continue
		}
		for _, item := range richText {
			text, ok := item["text"].(map[string]string)
			if ok {
				out.WriteString(text["content"])
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}
