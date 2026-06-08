package filenames

import (
	"strings"
	"testing"
	"time"

	"go_subtitle_whisper/internal/domain"
)

func TestWithSuffixUsesLibraryCodeAndDateOnly(t *testing.T) {
	task := &domain.Task{
		Title:  "\u673a\u5668\u5b66\u4e60\u8def\u7ebf\u56fe",
		Domain: "\u4eba\u5de5\u667a\u80fd",
	}
	got := WithSuffix(task, time.Date(2026, 6, 9, 12, 34, 56, 0, time.Local), ".md")

	want := "AIT-\u4eba\u5de5\u667a\u80fd-\u673a\u5668\u5b66\u4e60\u8def\u7ebf\u56fe-20260609.md"
	if got != want {
		t.Fatalf("filename = %q, want %q", got, want)
	}
	if strings.Contains(got, "123456") {
		t.Fatalf("filename should use date only, got %q", got)
	}
}
