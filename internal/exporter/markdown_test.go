package exporter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_subtitle_whisper/internal/domain"
)

func TestMarkdownExporterWritesSummaryToConfiguredDirectory(t *testing.T) {
	targetDir := t.TempDir()
	task := &domain.Task{
		ID:         "task123",
		Name:       "AI 课程总结",
		SourceURL:  "https://www.bilibili.com/video/BV1Ab411Q7xK",
		AuthorName: "测试UP",
		DomainTags: []string{"AI", "Go"},
		Summary:    "# Summary\n\nContent",
	}

	result, err := NewMarkdownFolderExporter(targetDir).ExportMarkdown(context.Background(), task, "", task.Summary)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" {
		t.Fatalf("status = %q, want success", result.Status)
	}
	if filepath.Dir(result.Target) != targetDir {
		t.Fatalf("target dir = %q, want %q", filepath.Dir(result.Target), targetDir)
	}

	content, err := os.ReadFile(result.Target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		`title: "AI 课程总结"`,
		`author: "测试UP"`,
		`source_url: "https://www.bilibili.com/video/BV1Ab411Q7xK"`,
		`# Summary`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected exported markdown to contain %q, got:\n%s", want, text)
		}
	}
}
