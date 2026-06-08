package exporter

import (
	"strings"
	"testing"

	"go_subtitle_whisper/internal/domain"
)

func TestObsidianContentLinksDomainAndTags(t *testing.T) {
	task := &domain.Task{
		Title:  "\u5e02\u573a\u5206\u6790",
		Domain: "\u7ecf\u6d4e",
		Tags:   []string{"\u5b8f\u89c2", "\u7ecf\u6d4e\u5206\u6790"},
	}

	content := buildObsidianContent(task, "body")

	for _, want := range []string{
		"- \u9886\u57df\uff1a[[\u7ecf\u6d4e]]",
		"- \u6807\u7b7e\uff1a[[\u5b8f\u89c2]] [[\u7ecf\u6d4e\u5206\u6790]]",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected obsidian content to contain %q, got:\n%s", want, content)
		}
	}
}
