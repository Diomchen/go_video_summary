package llm

import (
	"strings"
	"testing"
)

func TestRewriteSummarySourceLinkPrefersCanonicalBVIDURL(t *testing.T) {
	summary := strings.TrimSpace(`
# 示例标题
> 📺 **原视频直达：** [https://www.bilibili.com/video/{{BVID}}](https://www.bilibili.com/video/{{BVID}})
> ⚠️ 你提供的 BVID 还是占位写法 {{BVID}}，建议补充真实 BVID 以便直达原视频。

## 核心简介
正文
`)

	got := rewriteSummarySourceLink(summary, "https://www.bilibili.com/video/BVfake123", "BV1Ab411Q7xK")

	if strings.Contains(got, "{{BVID}}") {
		t.Fatalf("expected placeholder link to be replaced, got: %s", got)
	}
	if strings.Contains(got, "⚠️") {
		t.Fatalf("expected BVID warning to be removed, got: %s", got)
	}
	if !strings.Contains(got, "https://www.bilibili.com/video/BV1Ab411Q7xK") {
		t.Fatalf("expected canonical BVID link, got: %s", got)
	}
}

func TestRewriteSummarySourceLinkCanonicalizesValidBVIDFromSourceURL(t *testing.T) {
	summary := strings.TrimSpace(`
# 示例标题
> 📺 **原视频直达：** [https://www.bilibili.com/video/{{BVID}}](https://www.bilibili.com/video/{{BVID}})
`)

	sourceURL := "https://www.bilibili.com/video/BV9Xx411c7Yz?p=2"
	got := rewriteSummarySourceLink(summary, sourceURL, "")

	if !strings.Contains(got, "https://www.bilibili.com/video/BV9Xx411c7Yz") {
		t.Fatalf("expected canonical BVID URL from source URL, got: %s", got)
	}
	if strings.Contains(got, "?p=2") {
		t.Fatalf("expected query string to be removed from canonical URL, got: %s", got)
	}
}

func TestRewriteSummarySourceLinkReplacesLiteralBVIDPlaceholder(t *testing.T) {
	summary := strings.TrimSpace(`
# 示例标题
> 📺 **原视频直达：** [https://www.bilibili.com/video/bvid](https://www.bilibili.com/video/bvid)
`)

	got := rewriteSummarySourceLink(summary, "", "BV1Ab411Q7xK")

	if strings.Contains(strings.ToLower(got), "/video/bvid") {
		t.Fatalf("expected literal bvid placeholder to be replaced, got: %s", got)
	}
	if !strings.Contains(got, "https://www.bilibili.com/video/BV1Ab411Q7xK") {
		t.Fatalf("expected canonical BVID link, got: %s", got)
	}
}
