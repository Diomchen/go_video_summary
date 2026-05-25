package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_subtitle_whisper/internal/domain"
)

func saveTaskOutputs(task *domain.Task, outputDir string, saveAll bool) ([]string, string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return nil, "", nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, "", err
	}

	base := fmt.Sprintf("%s-%s-%s", task.ID, slugify(task.Name), time.Now().Format("20060102-150405"))
	var saved []string
	summaryPath := ""

	if saveAll && strings.TrimSpace(task.Transcript) != "" {
		path := filepath.Join(outputDir, base+".transcript.txt")
		if err := os.WriteFile(path, []byte(task.Transcript), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, path)
	}
	if saveAll && strings.TrimSpace(task.TranslatedText) != "" {
		path := filepath.Join(outputDir, base+".translated.txt")
		if err := os.WriteFile(path, []byte(task.TranslatedText), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, path)
	}
	if strings.TrimSpace(task.Summary) != "" {
		summaryPath = filepath.Join(outputDir, base+".summary.md")
		if err := os.WriteFile(summaryPath, []byte(task.Summary), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, summaryPath)
	}

	return saved, summaryPath, nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "task"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "task"
	}
	return out
}
