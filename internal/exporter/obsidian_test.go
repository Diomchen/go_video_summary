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

func TestBuildObsidianContentAddsGraphFriendlyTagsAndLinks(t *testing.T) {
	task := &domain.Task{
		ID:              "task123",
		Name:            "深度学习入门",
		SourceURL:       "https://www.bilibili.com/video/BV1Ab411Q7xK",
		CollectionName:  "机器学习合集",
		CollectionIndex: 3,
		AuthorName:      "测试UP",
		DomainTags:      []string{"AI", "深度学习"},
	}

	content := buildObsidianContent(task, "正文内容")

	for _, want := range []string{
		"tags:",
		`  - "AI"`,
		`  - "深度学习"`,
		`up: "测试UP"`,
		`bvid: "BV1Ab411Q7xK"`,
		"## 关联",
		"- UP主：[[UP/测试UP]]",
		"- 领域：[[AI]] [[深度学习]]",
		"- 标签：[[AI]] [[深度学习]]",
		"- 合集：[[合集/机器学习合集]]",
		"正文内容",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected obsidian content to contain %q, got:\n%s", want, content)
		}
	}
}

func TestObsidianExporterWritesByDomainAndUpdatesIndexes(t *testing.T) {
	vault := t.TempDir()
	task := &domain.Task{
		ID:         "task123",
		Name:       "fallback-title",
		Title:      "AI 课程总结",
		SourceLink: "https://www.bilibili.com/video/BV1Ab411Q7xK",
		UPName:     "测试UP",
		Domain:     "科技",
		Tags:       []string{"OpenAI", "B站"},
		Summary:    "# Summary\n\nContent",
	}

	result, err := NewObsidianExporter(vault, "").ExportMarkdown(context.Background(), task, "", task.Summary)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" {
		t.Fatalf("status = %q, want success", result.Status)
	}
	if filepath.Dir(result.Target) != filepath.Join(vault, "科技") {
		t.Fatalf("target dir = %q, want %q", filepath.Dir(result.Target), filepath.Join(vault, "科技"))
	}

	content, err := os.ReadFile(result.Target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		`title: "AI 课程总结"`,
		`source_link: "https://www.bilibili.com/video/BV1Ab411Q7xK"`,
		`up_name: "测试UP"`,
		`domain: "科技"`,
		`  - "OpenAI"`,
		`  - "B站"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected exported markdown to contain %q, got:\n%s", want, text)
		}
	}

	domainIndex := readText(t, filepath.Join(vault, "领域索引.md"))
	tagIndex := readText(t, filepath.Join(vault, "标签索引.md"))
	if !strings.Contains(domainIndex, "- [[科技]]") {
		t.Fatalf("domain index missing value: %s", domainIndex)
	}
	if !strings.Contains(tagIndex, "- [[OpenAI]]") || !strings.Contains(tagIndex, "- [[B站]]") {
		t.Fatalf("tag index missing values: %s", tagIndex)
	}
}

func TestObsidianExporterUsesReadableMetadataFilename(t *testing.T) {
	vault := t.TempDir()
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

	result, err := NewObsidianExporter(vault, "").ExportMarkdown(context.Background(), task, sourcePath, task.Summary)
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

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
