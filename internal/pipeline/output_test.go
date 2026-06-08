package pipeline

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go_subtitle_whisper/internal/domain"
)

func TestSaveTaskOutputsUsesReadableMetadataFilename(t *testing.T) {
	outputDir := t.TempDir()
	domainName := "\u4eba\u5de5\u667a\u80fd"
	title := "\u673a\u5668\u5b66\u4e60\u8def\u7ebf\u56fe"
	task := &domain.Task{
		ID:      "task123",
		Name:    "fallback-name",
		Title:   title,
		Domain:  domainName,
		Summary: "# Summary\n\nContent",
	}

	_, summaryPath, err := saveTaskOutputs(task, outputDir, false)
	if err != nil {
		t.Fatal(err)
	}

	filename := filepath.Base(summaryPath)
	for _, want := range []string{domainName, title} {
		if !strings.Contains(filename, want) {
			t.Fatalf("filename = %q, want it to contain %q", filename, want)
		}
	}
	if strings.Contains(filename, task.ID) {
		t.Fatalf("filename = %q, should not contain task id %q", filename, task.ID)
	}
	if !regexp.MustCompile(`^AIT-.*-\d{8}\.summary\.md$`).MatchString(filename) {
		t.Fatalf("filename = %q, want library-coded date summary markdown filename", filename)
	}
	if regexp.MustCompile(`\d{8}-\d{6}`).MatchString(filename) {
		t.Fatalf("filename = %q, should use date only", filename)
	}
}
