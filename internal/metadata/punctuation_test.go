package metadata

import "testing"

func TestExtractMetadataCleansDomainAndTagPunctuation(t *testing.T) {
	input := `<!-- metadata
{"domain":"` + "`" + `\u4eba\u5de5\u667a\u80fd` + "`" + `\uff1a","tags":["` + "`" + `OpenAI` + "`" + `","#\u89c6\u9891\uff0c","\u7ecf\u6d4e\uff1a\u5206\u6790"]}
-->
# T

body`

	_, meta := ExtractFromSummary(input)
	if meta.Domain != "\u4eba\u5de5\u667a\u80fd" {
		t.Fatalf("domain = %q, want clean domain", meta.Domain)
	}
	if len(meta.Tags) != 3 || meta.Tags[0] != "OpenAI" || meta.Tags[1] != "\u89c6\u9891" || meta.Tags[2] != "\u7ecf\u6d4e\u5206\u6790" {
		t.Fatalf("tags = %#v, want clean tags", meta.Tags)
	}
}
