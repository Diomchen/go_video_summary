package taxonomy

import (
	"fmt"
	"strings"
	"unicode"
)

type DomainCategory struct {
	Name    string
	Code    string
	Aliases []string
}

var topLevelDomains = []DomainCategory{
	{Name: "\u4eba\u5de5\u667a\u80fd", Code: "AIT", Aliases: []string{"ai", "\u673a\u5668\u5b66\u4e60", "\u6df1\u5ea6\u5b66\u4e60", "\u5f3a\u5316\u5b66\u4e60", "\u795e\u7ecf\u7f51\u7edc", "\u5927\u6a21\u578b", "llm"}},
	{Name: "\u7ecf\u6d4e", Code: "ECN", Aliases: []string{"\u91d1\u878d", "\u8d22\u653f", "\u5b8f\u89c2", "\u5fae\u89c2", "\u91cf\u5316\u7ecf\u6d4e", "\u7ecf\u6d4e\u5206\u6790", "\u7ecf\u6d4e\u65f6\u653f"}},
	{Name: "\u6295\u8d44", Code: "INV", Aliases: []string{"\u80a1\u7968", "\u57fa\u91d1", "\u503a\u5238", "\u671f\u8d27", "\u77ed\u7ebf\u4ea4\u6613", "\u4ea4\u6613", "\u7406\u8d22", "\u8d44\u4ea7\u914d\u7f6e", "\u6295\u673a"}},
	{Name: "\u79d1\u6280", Code: "TEC", Aliases: []string{"\u6570\u7801", "\u4e92\u8054\u7f51", "\u8f6f\u4ef6", "\u786c\u4ef6", "\u521b\u65b0", "\u6280\u672f"}},
	{Name: "\u7f16\u7a0b", Code: "DEV", Aliases: []string{"\u5f00\u53d1", "\u5de5\u7a0b", "\u4ee3\u7801", "golang", "go", "python", "javascript", "\u524d\u7aef", "\u540e\u7aef"}},
	{Name: "\u5546\u4e1a", Code: "BUS", Aliases: []string{"\u521b\u4e1a", "\u516c\u53f8", "\u7ba1\u7406", "\u8425\u9500", "\u589e\u957f", "\u4ea7\u54c1"}},
	{Name: "\u5386\u53f2", Code: "HIS", Aliases: []string{"\u4e16\u754c\u53f2", "\u4e2d\u56fd\u53f2", "\u53e4\u4ee3", "\u8fd1\u4ee3\u53f2"}},
	{Name: "\u5fc3\u7406\u5b66", Code: "PSY", Aliases: []string{"\u5fc3\u7406", "\u8ba4\u77e5", "\u60c5\u7eea", "\u5173\u7cfb", "\u6c9f\u901a"}},
	{Name: "\u6559\u80b2", Code: "EDU", Aliases: []string{"\u5b66\u4e60", "\u8bfe\u7a0b", "\u8003\u8bd5", "\u6559\u5b66"}},
	{Name: "\u5065\u5eb7", Code: "HEA", Aliases: []string{"\u533b\u5b66", "\u8fd0\u52a8", "\u8425\u517b", "\u7761\u7720"}},
	{Name: "\u827a\u672f", Code: "ART", Aliases: []string{"\u8bbe\u8ba1", "\u97f3\u4e50", "\u7535\u5f71", "\u6587\u5b66", "\u7f8e\u5b66"}},
}

func TopLevelDomains() []DomainCategory {
	out := make([]DomainCategory, len(topLevelDomains))
	copy(out, topLevelDomains)
	return out
}

func PromptChoices() string {
	parts := make([]string, 0, len(topLevelDomains))
	for _, item := range topLevelDomains {
		examples := item.Aliases
		if len(examples) > 3 {
			examples = examples[:3]
		}
		parts = append(parts, fmt.Sprintf("%s（%s）", item.Name, strings.Join(examples, "、")))
	}
	return strings.Join(parts, "；")
}

func NormalizeDomain(value string) string {
	value = cleanLabel(value)
	normalized := normalizeText(value)
	if normalized == "" {
		return ""
	}
	for _, item := range topLevelDomains {
		if normalized == normalizeText(item.Name) || containsAny(normalized, item.Aliases) {
			return item.Name
		}
	}
	return value
}

func DomainCode(domainName string) string {
	domainName = cleanLabel(domainName)
	if domainName == "" || domainName == "\u672a\u5206\u7c7b" {
		return "GEN"
	}
	normalized := NormalizeDomain(domainName)
	for _, item := range topLevelDomains {
		if normalized == item.Name {
			return item.Code
		}
	}
	return "OTH"
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		needle = normalizeText(needle)
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func normalizeText(value string) string {
	value = strings.ToLower(cleanLabel(value))
	replacer := strings.NewReplacer(" ", "", "\t", "", "-", "", "_", "")
	return replacer.Replace(value)
}

func cleanLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
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
	return strings.TrimSpace(b.String())
}
