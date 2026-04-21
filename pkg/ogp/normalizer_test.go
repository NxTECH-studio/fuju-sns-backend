package ogp

import "testing"

func TestNormalize_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases scheme and host", "HTTPS://Example.COM/a", "https://example.com/a"},
		{"drops default https port", "https://example.com:443/a", "https://example.com/a"},
		{"drops default http port", "http://example.com:80/a", "http://example.com/a"},
		{"keeps non-default port", "https://example.com:8443/a", "https://example.com:8443/a"},
		{"trims trailing dot from host", "https://example.com./a", "https://example.com/a"},
		{"strips fragment", "https://example.com/a#section", "https://example.com/a"},
		{"root slash preserved", "https://example.com/", "https://example.com/"},
		{"empty path becomes root", "https://example.com", "https://example.com/"},
		{"drops trailing slash on path", "https://example.com/a/b/", "https://example.com/a/b"},
		{"collapses repeated slashes", "https://example.com/a//b///c/", "https://example.com/a/b/c"},
		{"sorts query keys", "https://example.com/?b=2&a=1", "https://example.com/?a=1&b=2"},
		{"drops utm_source", "https://example.com/?utm_source=x&a=1", "https://example.com/?a=1"},
		{"drops gclid and fbclid", "https://example.com/?gclid=X&fbclid=Y", "https://example.com/"},
		{"spec example", "HTTPS://Example.COM:443/path//sub/?utm_source=tw&b=2&a=1#frag", "https://example.com/path/sub?a=1&b=2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.in)
			if err != nil {
				t.Fatalf("Normalize(%q): unexpected error %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalize_TrailingSlashEquivalence(t *testing.T) {
	a, err := Normalize("https://example.com/path/sub")
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := Normalize("https://example.com/path/sub/")
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a != b {
		t.Errorf("expected %q == %q", a, b)
	}
	if Hash(a) != Hash(b) {
		t.Errorf("hashes must match: %q vs %q", Hash(a), Hash(b))
	}
}

func TestNormalize_UnsupportedScheme(t *testing.T) {
	for _, raw := range []string{"ftp://x/", "javascript:alert(1)", "file:///etc/passwd", "mailto:a@b"} {
		if _, err := Normalize(raw); err == nil {
			t.Errorf("Normalize(%q) = nil err, want error", raw)
		}
	}
}

func TestNormalize_IDN(t *testing.T) {
	got, err := Normalize("https://日本.example/")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// idna should punycode the host. Just require that output is ASCII.
	for _, r := range got {
		if r > 0x7F {
			t.Errorf("output must be ASCII after idna, got %q", got)
			break
		}
	}
}

func TestNormalize_EmptyOrMalformed(t *testing.T) {
	for _, raw := range []string{"", "   ", "://nohost", "https://"} {
		if _, err := Normalize(raw); err == nil {
			t.Errorf("Normalize(%q) should error", raw)
		}
	}
}

func TestNormalize_IPv6KeepsBrackets(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://[2001:db8::1]:8080/a", "https://[2001:db8::1]:8080/a"},
		{"https://[2001:db8::1]/a", "https://[2001:db8::1]/a"},
		{"https://[::1]:8080/", "https://[::1]:8080/"},
	}
	for _, tc := range tests {
		got, err := Normalize(tc.in)
		if err != nil {
			t.Errorf("Normalize(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
