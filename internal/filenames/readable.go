package filenames

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"go_subtitle_whisper/internal/domain"
)

const (
	defaultDomain = "\u672a\u5206\u7c7b"
	defaultTitle  = "\u672a\u547d\u540d"
)

func Base(task *domain.Task, timestamp time.Time) string {
	domainName := safePart(firstNonEmpty(taskDomain(task), defaultDomain), 24)
	title := safePart(firstNonEmpty(taskTitle(task), defaultTitle), 72)
	return strings.Join([]string{domainName, title, timestamp.Format("20060102-150405")}, "-")
}

func WithSuffix(task *domain.Task, timestamp time.Time, suffix string) string {
	return Base(task, timestamp) + suffix
}

func SafeDir(value string) string {
	return safePart(value, 80)
}

func UniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for idx := 2; ; idx++ {
		candidate := fmt.Sprintf("%s-%d%s", base, idx, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func taskDomain(task *domain.Task) string {
	if task == nil {
		return ""
	}
	return task.Domain
}

func taskTitle(task *domain.Task) string {
	if task == nil {
		return ""
	}
	return firstNonEmpty(task.Title, task.Name, task.OriginalFileName)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safePart(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		if isInvalidPathRune(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	out := strings.Trim(b.String(), ". ")
	if out == "" {
		return ""
	}
	if limit > 0 {
		runes := []rune(out)
		if len(runes) > limit {
			out = strings.Trim(string(runes[:limit]), ". ")
		}
	}
	return out
}

func isInvalidPathRune(r rune) bool {
	if r < 32 {
		return true
	}
	switch r {
	case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
		return true
	default:
		return false
	}
}
