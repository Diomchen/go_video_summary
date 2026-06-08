package metadata

import "testing"

func TestExtractMetadataComment(t *testing.T) {
	input := `<!-- metadata
{"title":"T","source_link":"https://example.com","up_name":"UP","domain":"科技","tags":["OpenAI","B站"]}
-->
# T

body`
	clean, meta := ExtractFromSummary(input)
	if clean == "" || clean[0] == '<' {
		t.Fatalf("metadata comment was not removed: %q", clean)
	}
	if meta.Title != "T" || meta.SourceLink != "https://example.com" || meta.UPName != "UP" || meta.Domain != "科技" {
		t.Fatalf("metadata parsed incorrectly: %+v", meta)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "OpenAI" || meta.Tags[1] != "B站" {
		t.Fatalf("tags parsed incorrectly: %#v", meta.Tags)
	}
}

func TestExtractMarkdownFallback(t *testing.T) {
	input := "# A Better Title\n\n## 领域标签\n科技 | OpenAI | B站\n\n## 核心简介\ntext"
	clean, meta := ExtractFromSummary(input)
	if clean != input {
		t.Fatalf("fallback should not rewrite content")
	}
	if meta.Title != "A Better Title" || meta.Domain != "科技" {
		t.Fatalf("fallback metadata = %+v", meta)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "OpenAI" || meta.Tags[1] != "B站" {
		t.Fatalf("fallback tags = %#v", meta.Tags)
	}
}
