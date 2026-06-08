package knowledge

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go_subtitle_whisper/internal/metadata"
)

type ObsidianIndex struct {
	vaultDir            string
	domainIndexFile     string
	tagIndexFile        string
	similarityThreshold float64
}

func NewObsidianIndex(vaultDir, domainFile, tagFile string, threshold float64) *ObsidianIndex {
	if strings.TrimSpace(domainFile) == "" {
		domainFile = "领域索引.md"
	}
	if strings.TrimSpace(tagFile) == "" {
		tagFile = "标签索引.md"
	}
	if threshold <= 0 {
		threshold = 0.82
	}
	return &ObsidianIndex{
		vaultDir:            strings.TrimSpace(vaultDir),
		domainIndexFile:     domainFile,
		tagIndexFile:        tagFile,
		similarityThreshold: threshold,
	}
}

func (i *ObsidianIndex) Normalize(meta metadata.SummaryMetadata) (metadata.SummaryMetadata, error) {
	if strings.TrimSpace(i.vaultDir) == "" {
		return meta, fmt.Errorf("obsidian vault dir is required")
	}
	if err := os.MkdirAll(i.vaultDir, 0o755); err != nil {
		return meta, err
	}

	domain := strings.TrimSpace(meta.Domain)
	if domain == "" {
		domain = "未分类"
	}
	domains, err := readIndex(filepath.Join(i.vaultDir, i.domainIndexFile))
	if err != nil {
		return meta, err
	}
	domain, domains = bestMatchOrAppend(domains, domain, i.similarityThreshold)
	if err := writeIndex(filepath.Join(i.vaultDir, i.domainIndexFile), "领域索引", domains); err != nil {
		return meta, err
	}

	tags, err := readIndex(filepath.Join(i.vaultDir, i.tagIndexFile))
	if err != nil {
		return meta, err
	}
	normalizedTags := make([]string, 0, len(meta.Tags))
	for _, tag := range meta.Tags {
		tag = strings.TrimSpace(strings.Trim(tag, "#"))
		if tag == "" {
			continue
		}
		var normalized string
		normalized, tags = bestMatchOrAppend(tags, tag, i.similarityThreshold)
		normalizedTags = appendUnique(normalizedTags, normalized)
	}
	if err := writeIndex(filepath.Join(i.vaultDir, i.tagIndexFile), "标签索引", tags); err != nil {
		return meta, err
	}

	meta.Domain = domain
	meta.Tags = normalizedTags
	return meta, nil
}

func Similarity(a, b string) float64 {
	a = normalizeText(a)
	b = normalizeText(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		shorter := len([]rune(a))
		longer := len([]rune(b))
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		if longer == 0 {
			return 0
		}
		return 0.75 + 0.25*(float64(shorter)/float64(longer))
	}
	distance := levenshtein([]rune(a), []rune(b))
	maxLen := max(len([]rune(a)), len([]rune(b)))
	if maxLen == 0 {
		return 0
	}
	score := 1 - float64(distance)/float64(maxLen)
	if score < 0 {
		return 0
	}
	return score
}

func bestMatchOrAppend(values []string, candidate string, threshold float64) (string, []string) {
	best := ""
	bestScore := 0.0
	for _, value := range values {
		score := Similarity(value, candidate)
		if score > bestScore {
			best = value
			bestScore = score
		}
	}
	if best != "" && bestScore >= threshold {
		return best, values
	}
	values = appendUnique(values, candidate)
	return candidate, values
}

func readIndex(path string) ([]string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var values []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		values = appendUnique(values, value)
	}
	return values, scanner.Err()
}

func writeIndex(path, title string, values []string) error {
	sort.SliceStable(values, func(i, j int) bool {
		return strings.ToLower(values[i]) < strings.ToLower(values[j])
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", value)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return values
		}
	}
	return append(values, value)
}

func normalizeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "\t", "", "-", "", "_", "", "#", "", "，", "", ",", "", "。", "", ".", "")
	return replacer.Replace(value)
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
