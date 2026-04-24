package ogp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetcher_SuccessExtractsMetadata(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head>
		  <meta property="og:title" content="Sample">
		  <meta property="og:description" content="D">
		  <meta property="og:image" content="/card.png">
		</head></html>`))
	}))
	defer srv.Close()

	f := NewFetcher(&Options{
		UserAgent: "FujuTest/1.0",
		Timeout:   2 * time.Second,
		Client:    srv.Client(), // use test server's client so private dial is allowed
	})
	preview, err := f.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if preview.Title != "Sample" || preview.Description != "D" {
		t.Errorf("preview = %+v", preview)
	}
	if !strings.HasSuffix(preview.ImageURL, "/card.png") {
		t.Errorf("ImageURL should be absolute, got %q", preview.ImageURL)
	}
	if gotUA != "FujuTest/1.0" {
		t.Errorf("User-Agent = %q, want FujuTest/1.0", gotUA)
	}
	if preview.URLHash == "" {
		t.Error("URLHash must be populated")
	}
}

func TestFetcher_NonHTMLContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	f := NewFetcher(&Options{Client: srv.Client()})
	_, err := f.Fetch(context.Background(), srv.URL+"/api")
	if !errors.Is(err, ErrNotHTML) {
		t.Errorf("expected ErrNotHTML, got %v", err)
	}
	if Retriable(err) {
		t.Error("ErrNotHTML must not be retriable")
	}
}

func TestFetcher_5xxIsRetriable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	f := NewFetcher(&Options{Client: srv.Client()})
	_, err := f.Fetch(context.Background(), srv.URL+"/")
	var bs *ErrBadStatus
	if !errors.As(err, &bs) || bs.Status != http.StatusBadGateway {
		t.Fatalf("expected ErrBadStatus(502), got %v", err)
	}
	if !Retriable(err) {
		t.Error("5xx should be retriable")
	}
}

func TestFetcher_4xxNotRetriable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	f := NewFetcher(&Options{Client: srv.Client()})
	_, err := f.Fetch(context.Background(), srv.URL+"/")
	if err == nil {
		t.Fatal("expected error")
	}
	if Retriable(err) {
		t.Error("4xx should not be retriable")
	}
}

func TestFetcher_BodyCapAppliedOnOversizedResponse(t *testing.T) {
	big := strings.Repeat("A", int(MaxBodyBytes)+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>T</title></head><body>" + big + "</body></html>"))
	}))
	defer srv.Close()
	f := NewFetcher(&Options{Client: srv.Client()})
	// The cap is enforced at read time; the parser may still succeed
	// for the head section before the cap bites. Confirm no crash and
	// a non-empty title.
	preview, err := f.Fetch(context.Background(), srv.URL+"/")
	// Either success-with-title or a truncation error is acceptable;
	// the critical requirement is "don't hang or OOM".
	if err == nil && preview.Title == "" {
		t.Error("expected either a parsed title or a truncation error")
	}
}
