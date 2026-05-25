package source

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type BilibiliMedia struct {
	Title    string
	PageURL  string
	AudioURL string
	Ext      string
}

type BilibiliClient struct {
	httpClient *http.Client
	userAgent  string
}

func NewBilibiliClient(userAgent string, timeout time.Duration) *BilibiliClient {
	return &BilibiliClient{
		httpClient: &http.Client{Timeout: timeout},
		userAgent:  userAgent,
	}
}

func (c *BilibiliClient) Resolve(ctx context.Context, rawURL string) (*BilibiliMedia, error) {
	pageURL, err := normalizeBilibiliURL(rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("bilibili page request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	content := string(body)

	title := extractHTMLTitle(content)
	if title == "" {
		title = path.Base(strings.TrimRight(pageURL, "/"))
	}

	// Try inline __playinfo__ first (legacy pages).
	playInfo, inlineErr := extractPlayInfo(content)
	if inlineErr == nil {
		audioURL := selectBestAudio(playInfo)
		if audioURL != "" {
			return &BilibiliMedia{
				Title:    title,
				PageURL:  pageURL,
				AudioURL: audioURL,
				Ext:      extFromURL(audioURL),
			}, nil
		}
	}

	// Fallback: extract bvid+cid from __INITIAL_STATE__ and call the playurl API.
	bvid, cid, err := extractBvidCid(content, pageURL)
	if err != nil {
		return nil, err
	}
	playInfo, err = c.fetchPlayInfoAPI(ctx, bvid, cid)
	if err != nil {
		return nil, err
	}
	audioURL := selectBestAudio(playInfo)
	if audioURL == "" {
		return nil, fmt.Errorf("no audio stream found in bilibili playinfo")
	}

	return &BilibiliMedia{
		Title:    title,
		PageURL:  pageURL,
		AudioURL: audioURL,
		Ext:      extFromURL(audioURL),
	}, nil
}

func selectBestAudio(playInfo *playInfoResponse) string {
	bestBandwidth := -1
	audioURL := ""
	for _, audio := range playInfo.Data.Dash.Audio {
		candidate := firstNonEmpty(audio.BaseURL, audio.BaseUrl, firstBackup(audio.BackupURL))
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if audio.Bandwidth > bestBandwidth {
			bestBandwidth = audio.Bandwidth
			audioURL = candidate
		}
	}
	return strings.TrimSpace(audioURL)
}

func (c *BilibiliClient) fetchPlayInfoAPI(ctx context.Context, bvid, cid string) (*playInfoResponse, error) {
	apiURL := fmt.Sprintf("https://api.bilibili.com/x/player/playurl?bvid=%s&cid=%s&fnval=16&qn=64", bvid, cid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed playInfoResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse bilibili playurl API response: %w", err)
	}
	if parsed.Code != 0 {
		return nil, fmt.Errorf("bilibili playurl API error: code=%d msg=%s", parsed.Code, parsed.Message)
	}
	if len(parsed.Data.Dash.Audio) == 0 {
		return nil, fmt.Errorf("bilibili playurl API returned no audio streams")
	}
	return &parsed, nil
}

func (c *BilibiliClient) DownloadAudio(ctx context.Context, media *BilibiliMedia, destination string, onProgress func(float64)) error {
	if media == nil {
		return fmt.Errorf("missing media")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, media.AudioURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", media.PageURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("bilibili audio download failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()

	var written int64
	buf := make([]byte, 32*1024)
	total := resp.ContentLength
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
			if onProgress != nil && total > 0 {
				onProgress(float64(written) / float64(total) * 100)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if onProgress != nil {
		onProgress(100)
	}
	return nil
}

type playInfoResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Dash struct {
			Audio []struct {
				BaseURL   string   `json:"baseUrl"`
				BaseUrl   string   `json:"base_url"`
				BackupURL []string `json:"backup_url"`
				Bandwidth int      `json:"bandwidth"`
			} `json:"audio"`
		} `json:"dash"`
	} `json:"data"`
}

var (
	playInfoPrefix     = regexp.MustCompile(`(?s)(?:window\.__playinfo__|__playinfo__)\s*=\s*\{`)
	initialStatePrefix = regexp.MustCompile(`(?s)window\.__INITIAL_STATE__\s*=\s*\{`)
	titlePattern       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	bilibiliURLPattern = regexp.MustCompile(`(?i)\b(?:https?://|www\.)?(?:www\.)?(?:bilibili\.com/[^\s"'<>]+|b23\.tv/[^\s"'<>]+)\b`)
	bvidPattern        = regexp.MustCompile(`(?i)\b(BV[0-9A-Za-z]{10})\b`)
)

// extractBvidCid extracts the video's bvid and cid from __INITIAL_STATE__ in the page HTML.
// Falls back to extracting bvid from the URL and calling the bilibili API for cid.
func extractBvidCid(content, pageURL string) (string, string, error) {
	// Try extracting from __INITIAL_STATE__ JSON.
	loc := initialStatePrefix.FindStringIndex(content)
	if loc != nil {
		openIdx := strings.Index(content[loc[0]:], "{")
		if openIdx >= 0 {
			start := loc[0] + openIdx
			depth := 0
			end := -1
			for i := start; i < len(content); i++ {
				switch content[i] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						end = i
					}
				}
				if end >= 0 {
					break
				}
			}
			if end > 0 {
				raw := content[start : end+1]
				var state struct {
					VideoData struct {
						Bvid string `json:"bvid"`
						Aid  int64  `json:"aid"`
						Cid  int64  `json:"cid"`
					} `json:"videoData"`
				}
				if json.Unmarshal([]byte(raw), &state) == nil {
					if state.VideoData.Bvid != "" && state.VideoData.Cid > 0 {
						return state.VideoData.Bvid, fmt.Sprintf("%d", state.VideoData.Cid), nil
					}
				}
			}
		}
	}

	// Fallback: extract bvid from URL, then call view API for cid.
	bvid := ExtractBVID(pageURL)
	if bvid == "" {
		return "", "", fmt.Errorf("cannot extract bvid from bilibili page")
	}
	cid, err := fetchCidFromAPI(bvid)
	if err != nil {
		return "", "", fmt.Errorf("get cid for %s: %w", bvid, err)
	}
	return bvid, cid, nil
}

func fetchCidFromAPI(bvid string) (string, error) {
	apiURL := fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?bvid=%s", bvid)
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		Code int    `json:"code"`
		Data struct {
			Cid int64 `json:"cid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("bilibili view API error: code=%d", result.Code)
	}
	if result.Data.Cid == 0 {
		return "", fmt.Errorf("no cid found for bvid %s", bvid)
	}
	return fmt.Sprintf("%d", result.Data.Cid), nil
}

func extractPlayInfo(content string) (*playInfoResponse, error) {
	loc := playInfoPrefix.FindStringIndex(content)
	if loc == nil {
		return nil, fmt.Errorf("cannot find bilibili playinfo JSON")
	}
	openIdx := strings.Index(content[loc[0]:], "{")
	if openIdx < 0 {
		return nil, fmt.Errorf("cannot find bilibili playinfo JSON")
	}
	start := loc[0] + openIdx
	depth := 0
	end := -1
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("cannot find bilibili playinfo JSON: unbalanced braces")
	}
	raw := content[start : end+1]
	var parsed playInfoResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse bilibili playinfo: %w", err)
	}
	return &parsed, nil
}

func extractHTMLTitle(content string) string {
	matches := titlePattern.FindStringSubmatch(content)
	if len(matches) < 2 {
		return ""
	}
	title := html.UnescapeString(strings.TrimSpace(matches[1]))
	title = strings.TrimSuffix(title, "_bilibili")
	title = strings.TrimSuffix(title, "-bilibili")
	return strings.TrimSpace(title)
}

func normalizeBilibiliURL(rawURL string) (string, error) {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return "", fmt.Errorf("empty bilibili url")
	}
	if bvid := ExtractBVID(value); bvid != "" && !strings.Contains(strings.ToLower(value), "bilibili.com") && !strings.Contains(strings.ToLower(value), "b23.tv") {
		return BuildBilibiliVideoURL(bvid), nil
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(parsed.Host)
	if !strings.Contains(host, "bilibili.com") && host != "b23.tv" {
		return "", fmt.Errorf("unsupported host: %s", parsed.Host)
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func BuildBilibiliVideoURL(bvid string) string {
	return "https://www.bilibili.com/video/" + normalizeBVID(bvid)
}

func ExtractBilibiliInputs(raw string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)

	add := func(value string) {
		normalized, err := normalizeBilibiliURL(value)
		if err != nil {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	for _, match := range bilibiliURLPattern.FindAllString(raw, -1) {
		add(match)
	}

	cleaned := bilibiliURLPattern.ReplaceAllString(raw, " ")
	for _, match := range bvidPattern.FindAllStringSubmatch(cleaned, -1) {
		if len(match) < 2 {
			continue
		}
		add(BuildBilibiliVideoURL(match[1]))
	}

	return out
}

func ExtractBVID(rawURL string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)/video/(BV[0-9A-Za-z]{10})`),
		regexp.MustCompile(`(?i)\b(BV[0-9A-Za-z]{10})\b`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(strings.TrimSpace(rawURL))
		if len(matches) >= 2 {
			return normalizeBVID(matches[1])
		}
	}
	return ""
}

func normalizeBVID(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return raw
	}
	return "BV" + raw[2:]
}

func extFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ".m4a"
	}
	ext := strings.ToLower(filepath.Ext(parsed.Path))
	if ext == "" {
		return ".m4a"
	}
	return ext
}

func firstBackup(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return firstNonEmpty(values[0])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
