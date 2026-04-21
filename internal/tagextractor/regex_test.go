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
	// Deterministic order: sorted by keyword name → ["fuju", "golang"].
	want := []string{"fuju", "golang"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
}

// TestRegex_KeywordDictionary_WordBoundary verifies "fuji" does NOT match
// inside "fujitsu" — the review flagged substring matching as wrong.
func TestRegex_KeywordDictionary_WordBoundary(t *testing.T) {
	ex := NewRegexTagExtractor([]string{"fuji"})
	got, err := ex.Extract(context.Background(), "I bought a fujitsu laptop")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no match on 'fujitsu', got %v", got)
	}

	got2, err := ex.Extract(context.Background(), "climbed fuji yesterday")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got2) != 1 || got2[0] != "fuji" {
		t.Errorf("expected [fuji], got %v", got2)
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
