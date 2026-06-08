package exporter

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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

func TestMarkdownExporterUsesReadableMetadataFilename(t *testing.T) {
	targetDir := t.TempDir()
	domainName := "\u4eba\u5de5\u667a\u80fd"
	title := "\u673a\u5668\u5b66\u4e60\u8def\u7ebf\u56fe"
	task := &domain.Task{
		ID:      "task123",
		Name:    "fallback-name",
		Title:   title,
		Domain:  domainName,
		Summary: "# Summary\n\nContent",
	}
	sourcePath := filepath.Join(t.TempDir(), "task123-fallback-name-20260101-010203.summary.md")

	result, err := NewMarkdownFolderExporter(targetDir).ExportMarkdown(context.Background(), task, sourcePath, task.Summary)
	if err != nil {
		t.Fatal(err)
	}

	filename := filepath.Base(result.Target)
	for _, want := range []string{domainName, title} {
		if !strings.Contains(filename, want) {
			t.Fatalf("filename = %q, want it to contain %q", filename, want)
		}
	}
	if strings.Contains(filename, task.ID) {
		t.Fatalf("filename = %q, should not contain task id %q", filename, task.ID)
	}
	if !regexp.MustCompile(`^AIT-.*-\d{8}\.md$`).MatchString(filename) {
		t.Fatalf("filename = %q, want library-coded date markdown filename", filename)
	}
	if regexp.MustCompile(`\d{8}-\d{6}`).MatchString(filename) {
		t.Fatalf("filename = %q, should use date only", filename)
	}
}
