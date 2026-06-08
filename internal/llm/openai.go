package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go_subtitle_whisper/internal/metadata"
	"go_subtitle_whisper/internal/service"
	"go_subtitle_whisper/internal/source"
	"go_subtitle_whisper/internal/taxonomy"
)

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey, model string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Translate(ctx context.Context, input, sourceLanguage string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	system := "You are a subtitle translator. Translate the user's subtitle text into concise, natural Simplified Chinese. Preserve factual meaning and keep line length compact for subtitle reading."
	if sourceLanguage != "" {
		system += " The source language is likely " + sourceLanguage + "."
	}
	return c.chat(ctx, system, input)
}

func (c *Client) Summarize(ctx context.Context, transcript string, options service.SummaryOptions) (string, metadata.SummaryMetadata, error) {
	if strings.TrimSpace(transcript) == "" {
		return "", metadata.SummaryMetadata{}, nil
	}

	system := strings.Join([]string{
		"你是一位拥有百万粉丝的 B 站硬核知识区博主，擅长把冗长、带广告的视频字幕重组成一份逻辑严密、文风有趣、排版优雅的深度笔记。",
		"请根据用户提供的视频字幕内容输出一份高质量总结，整体使用简体中文 Markdown。",
		"先执行内容清洗，再输出总结。",
		"广告清理：自动识别并剔除所有商业推广内容，例如赞助商感谢、App 或课程推销、购物链接等，确保总结内容纯净。",
		"内容去水：过滤掉视频中的口语废话、语气词和无意义互动，只保留高价值信息。",
		"必须严格按以下格式输出：",
		"1. 用一级标题生成一个极具吸引力且点明主旨的主标题。标题下方紧跟这行 Markdown：> 📺 **原视频直达：** [https://www.bilibili.com/video/{{BVID}}](https://www.bilibili.com/video/{{BVID}})。如果没有 BVID，请保留 {{BVID}} 占位符并提醒补充。",
		"2. 在主标题和原视频直达链接之后，用「领域标签」作为二级标题，在该标题下用一行输出 1-3 个领域标签，格式为：`顶层领域 | 具体标签1 | 具体标签2`。第一个标签必须是有且只有一个顶层领域，优先从这些候选中选择，不要输出更细的子领域作为第一个标签：" + taxonomy.PromptChoices() + "。后续标签可写更具体的主题词。",
		"3. 用「核心简介」作为二级标题，写一段约 150 字的文字，概括视频的核心价值以及它解决了什么问题。",
		"4. 用「逻辑骨架」作为二级标题，使用 mermaid 的 graph TD 绘制一份逻辑图，清晰展示视频的主干流程。",
		"5. 用「深度复盘」作为二级标题，按照视频逻辑模块继续使用 ## 二级标题分模块总结。每个模块以大段落深度叙事为主，拒绝过度细碎的 1.1.1/1.1.2 分点。若涉及并列关系或步骤，可以在段落内部适当用 1. 2. 或 * 做简短引导，但核心仍是深入的文字解释。",
		"6. 风格要求：语气要生动风趣，像聪明人在茶余饭后的分享，但结论必须严谨客观。适量加入 Emoji 表情来增强视觉动感,在描述关键逻辑转换或重要结论时，必须配以形象的 Emoji（如 🧠, 🛠️, 💡, ⚠️），并对核心观点、关键术语和金句进行加粗。",
		"7. 用「极简锐评」作为二级标题，在文末写一段 50 字以内的犀利评价，点出这个视频最大的优点、局限性或终极洞察。",
		"8. 剔除广告和废话后，必须保持前后逻辑自然衔接，不要让内容出现断层。",
		"不要输出多余前言，不要省略任何要求的章节。",
	}, "\n")

	var prompt strings.Builder
	prompt.WriteString("请处理以下视频字幕内容。\n")
	if strings.TrimSpace(options.Title) != "" {
		prompt.WriteString("视频标题：")
		prompt.WriteString(strings.TrimSpace(options.Title))
		prompt.WriteString("\n")
	}
	if strings.TrimSpace(options.AuthorName) != "" {
		prompt.WriteString("UP主：")
		prompt.WriteString(strings.TrimSpace(options.AuthorName))
		prompt.WriteString("\n")
	}
	if strings.TrimSpace(options.SourceURL) != "" {
		prompt.WriteString("视频来源链接：")
		prompt.WriteString(strings.TrimSpace(options.SourceURL))
		prompt.WriteString("\n")
	}
	if strings.TrimSpace(options.BVID) != "" {
		prompt.WriteString("BVID：")
		prompt.WriteString(strings.TrimSpace(options.BVID))
		prompt.WriteString("\n")
	} else {
		prompt.WriteString("BVID：未提供，请在主标题下方的原片链接处保留 {{BVID}} 并提醒补充。\n")
	}
	if strings.TrimSpace(options.CollectionName) != "" {
		prompt.WriteString("所属合集：")
		prompt.WriteString(strings.TrimSpace(options.CollectionName))
		prompt.WriteString("\n")
	}
	if options.CollectionIndex > 0 {
		prompt.WriteString("合集序号：第 ")
		prompt.WriteString(fmt.Sprintf("%d", options.CollectionIndex))
		prompt.WriteString(" 集\n")
	}
	prompt.WriteString("\n待处理字幕内容如下：\n")
	prompt.WriteString(transcript)

	if strings.TrimSpace(options.PromptOverride) != "" {
		system = strings.TrimSpace(options.PromptOverride)
	}

	result, err := c.chat(ctx, system, prompt.String())
	if err != nil {
		return "", metadata.SummaryMetadata{}, err
	}
	result = rewriteSummarySourceLink(result, options.SourceURL, options.BVID)
	clean, meta := metadata.ExtractFromSummary(result)
	return clean, meta, nil
}

var (
	summaryPlaceholderLinkPattern        = regexp.MustCompile(`https://www\.bilibili\.com/video/\{\{BVID\}\}`)
	summaryLiteralPlaceholderLinkPattern = regexp.MustCompile(`(?i)https://www\.bilibili\.com/video/bvid\b`)
	summaryBVIDLinkPattern               = regexp.MustCompile(`https://www\.bilibili\.com/video/[Bb][Vv][0-9A-Za-z]{10}(?:[^\s)\]]*)?`)
	summaryBVIDWarningPattern            = regexp.MustCompile(`(?m)^>?\s*⚠️.*BVID.*(?:\r?\n)?`)
)

func rewriteSummarySourceLink(summary, sourceURL, bvid string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return summary
	}

	canonicalURL := canonicalSummarySourceURL(sourceURL, bvid)

	if canonicalURL != "" {
		lines := strings.Split(summary, "\n")
		for idx, line := range lines {
			if !strings.Contains(line, "原视频直达") {
				continue
			}
			updated := summaryPlaceholderLinkPattern.ReplaceAllString(line, canonicalURL)
			updated = summaryLiteralPlaceholderLinkPattern.ReplaceAllString(updated, canonicalURL)
			updated = summaryBVIDLinkPattern.ReplaceAllString(updated, canonicalURL)
			lines[idx] = updated
		}
		summary = strings.Join(lines, "\n")
		summary = summaryBVIDWarningPattern.ReplaceAllString(summary, "")
		return strings.TrimSpace(summary)
	}

	if resolvedBVID := source.ExtractBVID(bvid); resolvedBVID != "" {
		target := source.BuildBilibiliVideoURL(resolvedBVID)
		summary = summaryPlaceholderLinkPattern.ReplaceAllString(summary, target)
		summary = summaryLiteralPlaceholderLinkPattern.ReplaceAllString(summary, target)
		summary = strings.TrimSpace(summary)
	}
	return summary
}

var domainTagPattern = regexp.MustCompile(`(?m)^##\s*领域标签\s*\n\s*(.+)$`)

func extractDomainTags(summary string) []string {
	matches := domainTagPattern.FindStringSubmatch(summary)
	if len(matches) < 2 {
		return nil
	}
	raw := strings.TrimSpace(matches[1])
	parts := strings.Split(raw, "|")
	var tags []string
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func canonicalSummarySourceURL(sourceURL, bvid string) string {
	if resolvedBVID := source.ExtractBVID(bvid); resolvedBVID != "" {
		return source.BuildBilibiliVideoURL(resolvedBVID)
	}
	if resolvedBVID := source.ExtractBVID(sourceURL); resolvedBVID != "" {
		return source.BuildBilibiliVideoURL(resolvedBVID)
	}
	return strings.TrimSpace(sourceURL)
}

func (c *Client) chat(ctx context.Context, system, user string) (string, error) {
	if c.model == "" {
		return "", fmt.Errorf("llm model is empty")
	}
	reqBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0.2,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm request failed: %s", strings.TrimSpace(string(respData)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respData, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm response contained no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
