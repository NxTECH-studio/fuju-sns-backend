package storage

import "testing"

// TestSanitizeFilename covers the path-traversal stripping and the
// fallback for filenames that reduce to nothing meaningful. The Upload
// path joins this result under `images/{userID}/{ulid}/...`, so any
// regression here would let a malicious filename either escape the user
// prefix or land as an empty key segment.
func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"simple filename is preserved", "cat.jpg", "cat.jpg"},
		{"unix path traversal is stripped", "../../../etc/passwd", "passwd"},
		{"unix relative path is stripped", "subdir/foo.png", "foo.png"},
		{"windows path traversal is stripped", "..\\..\\windows\\system32\\evil.exe", "evil.exe"},
		{"mixed separators are stripped", "..\\foo/bar\\baz.png", "baz.png"},
		{"empty input falls back to file", "", "file"},
		{"single dot falls back to file", ".", "file"},
		{"single slash falls back to file", "/", "file"},
		{"japanese filename is preserved", "ねこ.jpg", "ねこ.jpg"},
		{"japanese path is stripped to leaf", "../親ディレクトリ/ねこ.jpg", "ねこ.jpg"},
		{"trailing slash drops to base", "foo/", "foo"},
		// `../../` collapses to `..` via filepath.Base, which is not
		// treated as an escape (S3 keys are flat strings) but documents
		// the current behavior — kept as-is to avoid scope creep.
		{"trailing slashes leave base segment", "../../", ".."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFilename(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
