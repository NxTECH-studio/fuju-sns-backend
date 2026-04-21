package domain

import (
	"strings"
	"testing"
)

func TestBadgeValidate(t *testing.T) {
	cases := []struct {
		name    string
		badge   Badge
		wantErr bool
	}{
		{name: "valid", badge: Badge{Key: "developer", Label: "開発者"}, wantErr: false},
		{name: "valid with https icon", badge: Badge{Key: "x", Label: "y", IconURL: "https://ex/a.png"}, wantErr: false},
		{name: "empty key", badge: Badge{Key: "", Label: "x"}, wantErr: true},
		{name: "empty label", badge: Badge{Key: "x", Label: ""}, wantErr: true},
		{name: "key too long", badge: Badge{Key: strings.Repeat("a", 65), Label: "x"}, wantErr: true},
		{name: "label too long (runes)", badge: Badge{Key: "x", Label: strings.Repeat("あ", 65)}, wantErr: true},
		{name: "label at rune limit", badge: Badge{Key: "x", Label: strings.Repeat("あ", 64)}, wantErr: false},
		{name: "javascript icon rejected", badge: Badge{Key: "x", Label: "y", IconURL: "javascript:alert(1)"}, wantErr: true},
		{name: "relative icon rejected", badge: Badge{Key: "x", Label: "y", IconURL: "/relative.png"}, wantErr: true},
		{name: "icon too long", badge: Badge{Key: "x", Label: "y", IconURL: "https://ex/" + strings.Repeat("a", 1024)}, wantErr: true},
		{name: "color too long", badge: Badge{Key: "x", Label: "y", Color: strings.Repeat("a", 17)}, wantErr: true},
		{name: "negative priority", badge: Badge{Key: "x", Label: "y", Priority: -1}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.badge.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("wantErr=%v got err=%v", tc.wantErr, err)
			}
		})
	}
}

func TestGrantBadgeRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     GrantBadgeRequest
		wantErr bool
	}{
		{name: "valid", req: GrantBadgeRequest{BadgeKey: "developer"}, wantErr: false},
		{name: "empty key", req: GrantBadgeRequest{BadgeKey: ""}, wantErr: true},
		{name: "key too long", req: GrantBadgeRequest{BadgeKey: strings.Repeat("a", 65)}, wantErr: true},
		{name: "reason too long", req: GrantBadgeRequest{BadgeKey: "x", Reason: strings.Repeat("a", 256)}, wantErr: true},
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
