package domain

import "testing"

func TestFollow_Validate(t *testing.T) {
	tests := []struct {
		name    string
		follow  Follow
		wantErr bool
	}{
		{"ok", Follow{FollowerSub: "a", FolloweeSub: "b"}, false},
		{"empty follower", Follow{FolloweeSub: "b"}, true},
		{"empty followee", Follow{FollowerSub: "a"}, true},
		{"self follow", Follow{FollowerSub: "a", FolloweeSub: "a"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.follow.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
