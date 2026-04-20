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
		{name: "empty key", badge: Badge{Key: "", Label: "x"}, wantErr: true},
		{name: "empty label", badge: Badge{Key: "x", Label: ""}, wantErr: true},
		{name: "key too long", badge: Badge{Key: strings.Repeat("a", 65), Label: "x"}, wantErr: true},
		{name: "label too long (runes)", badge: Badge{Key: "x", Label: strings.Repeat("あ", 65)}, wantErr: true},
		{name: "label at rune limit", badge: Badge{Key: "x", Label: strings.Repeat("あ", 64)}, wantErr: false},
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
