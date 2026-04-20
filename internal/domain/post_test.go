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
	cases := []struct {
		name    string
		req     CreatePostRequest
		wantErr bool
	}{
		{name: "valid", req: CreatePostRequest{Content: "hi"}, wantErr: false},
		{name: "4 images", req: CreatePostRequest{Content: "hi", ImageIDs: []string{"a", "b", "c", "d"}}, wantErr: false},
		{name: "5 images (too many)", req: CreatePostRequest{Content: "hi", ImageIDs: []string{"a", "b", "c", "d", "e"}}, wantErr: true},
		{name: "empty content", req: CreatePostRequest{Content: ""}, wantErr: true},
		{name: "121 runes", req: CreatePostRequest{Content: strings.Repeat("a", 121)}, wantErr: true},
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
