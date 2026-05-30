package exporter

import (
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
		`  - "bilibili"`,
		`  - "up/测试UP"`,
		`  - "domain/AI"`,
		`up: "测试UP"`,
		`bvid: "BV1Ab411Q7xK"`,
		"## 关联",
		"- UP主：[[UP/测试UP]]",
		"- 领域：[[领域/AI]] [[领域/深度学习]]",
		"- 合集：[[合集/机器学习合集]]",
		"正文内容",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected obsidian content to contain %q, got:\n%s", want, content)
		}
	}
}
