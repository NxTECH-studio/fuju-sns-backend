package domain

import (
	"strings"
	"testing"
)

func TestPostValidate(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name    string
		post    Post
		wantErr bool
	}{
		{name: "valid", post: Post{UserID: "u1", Content: "hi"}, wantErr: false},
		{name: "empty content", post: Post{UserID: "u1", Content: ""}, wantErr: true},
		{name: "121 runes", post: Post{UserID: "u1", Content: strings.Repeat("a", 121)}, wantErr: true},
		{name: "120 runes exactly", post: Post{UserID: "u1", Content: strings.Repeat("a", 120)}, wantErr: false},
		{name: "120 runes of kanji", post: Post{UserID: "u1", Content: strings.Repeat("あ", 120)}, wantErr: false},
		{name: "empty user_id", post: Post{UserID: "", Content: "hi"}, wantErr: true},
		{name: "empty parent_post_id", post: Post{UserID: "u1", Content: "hi", ParentPostID: strPtr("")}, wantErr: true},
		{name: "valid reply", post: Post{UserID: "u1", Content: "hi", ParentPostID: strPtr("01HX")}, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.post.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("wantErr=%v got err=%v", tc.wantErr, err)
			}
		})
	}
}

func TestCreatePostRequestValidate(t *testing.T) {
	// Every char here is valid Crockford Base32 (no I/L/O/U) and strings
	// are exactly 26 chars.
	// Crockford Base32 excludes I/L/O/U, so these fixed "ULIDs" only use
	// 0-9 + A-H/J/K/M/N/P-T/V-Z.
	ulids4 := []string{
		"01HXPST000000000000000AAAA",
		"01HXPST000000000000000BBBB",
		"01HXPST000000000000000CCCC",
		"01HXPST000000000000000DDDD",
	}
	ulids5 := append(ulids4, "01HXPST000000000000000EEEE")
	dup := []string{ulids4[0], ulids4[0]}
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name    string
		req     CreatePostRequest
		wantErr bool
	}{
		{name: "valid", req: CreatePostRequest{Content: "hi"}, wantErr: false},
		{name: "4 valid ulid images", req: CreatePostRequest{Content: "hi", ImageIDs: ulids4}, wantErr: false},
		{name: "5 images (too many)", req: CreatePostRequest{Content: "hi", ImageIDs: ulids5}, wantErr: true},
		{name: "non-ULID image_id", req: CreatePostRequest{Content: "hi", ImageIDs: []string{"img-1"}}, wantErr: true},
		{name: "duplicate image_ids", req: CreatePostRequest{Content: "hi", ImageIDs: dup}, wantErr: true},
		{name: "empty content", req: CreatePostRequest{Content: ""}, wantErr: true},
		{name: "121 runes", req: CreatePostRequest{Content: strings.Repeat("a", 121)}, wantErr: true},
		{name: "non-ULID parent", req: CreatePostRequest{Content: "hi", ParentPostID: strPtr("not-a-ulid")}, wantErr: true},
		{name: "valid parent ULID", req: CreatePostRequest{Content: "hi", ParentPostID: strPtr("01HXPST000000000000000PRNT")}, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("wantErr=%v got err=%v", tc.wantErr, err)
			}
		})
	}
}
