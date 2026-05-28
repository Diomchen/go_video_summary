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

func TestIsCollectionURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"season list", "https://space.bilibili.com/3546691177286448/lists/5942172?type=season", true},
		{"season list no query", "https://space.bilibili.com/12345/lists/67890", true},
		{"medialist play", "https://www.bilibili.com/medialist/play/12345", true},
		{"playlist", "https://www.bilibili.com/playlist/pl12345", true},
		{"single video", "https://www.bilibili.com/video/BV1Ab411Q7xK", false},
		{"b23 short link", "https://b23.tv/abcdef", false},
		{"bare bvid", "BV1Ab411Q7xK", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCollectionURL(tt.url)
			if got != tt.want {
				t.Errorf("IsCollectionURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestExtractCollectionIDs(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantMid  string
		wantSID  string
	}{
		{"space lists", "https://space.bilibili.com/3546691177286448/lists/5942172?type=season", "3546691177286448", "5942172"},
		{"medialist play", "https://www.bilibili.com/medialist/play/12345", "", "12345"},
		{"playlist", "https://www.bilibili.com/playlist/pl67890", "", "67890"},
		{"non collection", "https://www.bilibili.com/video/BV1Ab411Q7xK", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mid, sid := extractCollectionIDs(tt.url)
			if mid != tt.wantMid {
				t.Errorf("mid = %q, want %q", mid, tt.wantMid)
			}
			if sid != tt.wantSID {
				t.Errorf("sid = %q, want %q", sid, tt.wantSID)
			}
		})
	}
}
