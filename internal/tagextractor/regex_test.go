package tagextractor

import (
	"context"
	"reflect"
	"testing"
)

func TestRegex_ExtractHashtags(t *testing.T) {
	ex := NewRegexTagExtractor(nil)
	got, err := ex.Extract(context.Background(), "hello #Golang and #fuju_sns")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"golang", "fuju_sns"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
}

func TestRegex_DedupAndNormalize(t *testing.T) {
	ex := NewRegexTagExtractor(nil)
	got, err := ex.Extract(context.Background(), "#Fuju #fuju #FUJU")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"fuju"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
}

func TestRegex_UnicodeHashtag(t *testing.T) {
	ex := NewRegexTagExtractor(nil)
	got, err := ex.Extract(context.Background(), "投稿テスト #日本語タグ")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0] != "日本語タグ" {
		t.Errorf("unexpected: %v", got)
	}
}

func TestRegex_KeywordDictionary(t *testing.T) {
	ex := NewRegexTagExtractor([]string{"Golang", " Fuju "})
	got, err := ex.Extract(context.Background(), "I love golang and fuju is cool")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Keyword order in map iteration isn't deterministic — check set equality.
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	if !set["golang"] || !set["fuju"] {
		t.Errorf("expected both golang and fuju in %v", got)
	}
}

func TestRegex_MaxTagsCap(t *testing.T) {
	ex := NewRegexTagExtractor(nil)
	content := "#a #b #c #d #e #f #g #h #i #j #k #l"
	got, err := ex.Extract(context.Background(), content)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 10 {
		t.Errorf("expected 10 tags (cap), got %d: %v", len(got), got)
	}
}
