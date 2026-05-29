package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

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
		{"watch later", "https://www.bilibili.com/watchlater/list#/list", true},
		{"watch later no fragment", "https://www.bilibili.com/watchlater/list", true},
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

func TestExtractBilibiliInputsKeepsWatchLaterFragmentlessCanonicalURL(t *testing.T) {
	input := "稍后再看：https://www.bilibili.com/watchlater/list#/list"

	got := ExtractBilibiliInputs(input)

	if len(got) != 1 {
		t.Fatalf("expected 1 input, got %d: %#v", len(got), got)
	}
	if got[0] != "https://www.bilibili.com/watchlater/list" {
		t.Fatalf("unexpected watchlater URL: %q", got[0])
	}
}

func TestExtractCollectionIDs(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantMid string
		wantSID string
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

func TestResolveCollectionFetchesWatchLaterWithCachedCookie(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/x/v2/history/toview/web", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "SESSDATA=test-session" {
			t.Fatalf("expected cached cookie, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": {
				"list": [
					{"bvid":"BV1Ab411Q7xK","title":"first video","owner":{"name":"alice"}},
					{"bvid":"BV9Xy411A7Bc","title":"second video"}
				]
			}
		}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewBilibiliClient("test-agent", time.Second)
	client.apiBaseURL = server.URL
	client.cookieCache = NewBilibiliCookieCache(filepath.Join(t.TempDir(), "cookie.json"), 30*24*time.Hour)
	if err := client.cookieCache.Save("SESSDATA=test-session", time.Now()); err != nil {
		t.Fatal(err)
	}

	collection, err := client.ResolveCollection(context.Background(), "https://www.bilibili.com/watchlater/list#/list")
	if err != nil {
		t.Fatal(err)
	}

	if collection.Name != "稍后再看" {
		t.Fatalf("unexpected collection name: %q", collection.Name)
	}
	if collection.Author != "alice" {
		t.Fatalf("unexpected author: %q", collection.Author)
	}
	if len(collection.Videos) != 2 {
		t.Fatalf("expected 2 videos, got %d", len(collection.Videos))
	}
	if collection.Videos[0].PageURL != "https://www.bilibili.com/video/BV1Ab411Q7xK" {
		t.Fatalf("unexpected first page URL: %q", collection.Videos[0].PageURL)
	}
}

func TestBilibiliCookieCacheExpiresAfterTTL(t *testing.T) {
	cache := NewBilibiliCookieCache(filepath.Join(t.TempDir(), "cookie.json"), 30*24*time.Hour)
	if err := cache.Save("SESSDATA=old", time.Now().Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	cookie, ok := cache.Load()

	if ok {
		t.Fatalf("expected expired cookie to be ignored, got %q", cookie)
	}
}

func TestParseQRCodePollStatus(t *testing.T) {
	tests := []struct {
		name   string
		code   int
		status BilibiliQRCodeStatus
		done   bool
	}{
		{"success", 0, BilibiliQRCodeSucceeded, true},
		{"not scanned", 86101, BilibiliQRCodeWaiting, false},
		{"scanned", 86090, BilibiliQRCodeScanned, false},
		{"expired", 86038, BilibiliQRCodeExpired, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, done := parseQRCodePollStatus(tt.code)
			if status != tt.status {
				t.Fatalf("status = %q, want %q", status, tt.status)
			}
			if done != tt.done {
				t.Fatalf("done = %v, want %v", done, tt.done)
			}
		})
	}
}
