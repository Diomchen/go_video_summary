package source

import "testing"

func TestExtractBVIDPreservesLetterCase(t *testing.T) {
	input := "https://www.bilibili.com/video/BV1Ab411Q7xK?p=3"

	got := ExtractBVID(input)

	if got != "BV1Ab411Q7xK" {
		t.Fatalf("expected original case to be preserved, got %q", got)
	}
}

func TestExtractBilibiliInputsBuildsCanonicalURLFromRawBVID(t *testing.T) {
	input := "请处理这两个视频：BV1Ab411Q7xK 和 BV9Xy411A7Bc"

	got := ExtractBilibiliInputs(input)

	if len(got) != 2 {
		t.Fatalf("expected 2 inputs, got %d: %#v", len(got), got)
	}
	if got[0] != "https://www.bilibili.com/video/BV1Ab411Q7xK" {
		t.Fatalf("unexpected first canonical URL: %q", got[0])
	}
	if got[1] != "https://www.bilibili.com/video/BV9Xy411A7Bc" {
		t.Fatalf("unexpected second canonical URL: %q", got[1])
	}
}

func TestExtractBVIDNormalizesPrefixToUppercaseBV(t *testing.T) {
	input := "https://www.bilibili.com/video/bV1Ab411Q7xK"

	got := ExtractBVID(input)

	if got != "BV1Ab411Q7xK" {
		t.Fatalf("expected BV prefix to be normalized, got %q", got)
	}
}
