package metadata

import "testing"

func TestExtractMetadataCleansDomainAndTagPunctuation(t *testing.T) {
	input := `<!-- metadata
{"domain":"` + "`" + `\u4eba\u5de5\u667a\u80fd` + "`" + `\uff1a","tags":["` + "`" + `OpenAI` + "`" + `","#\u89c6\u9891\uff0c"]}
-->
# T

body`

	_, meta := ExtractFromSummary(input)
	if meta.Domain != "\u4eba\u5de5\u667a\u80fd" {
		t.Fatalf("domain = %q, want clean domain", meta.Domain)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "OpenAI" || meta.Tags[1] != "\u89c6\u9891" {
		t.Fatalf("tags = %#v, want clean tags", meta.Tags)
	}
}
