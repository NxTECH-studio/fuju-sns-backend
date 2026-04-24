// Package sharedcursor holds cursor encoder/decoder helpers shared
// between repository backends. Keeping these in one place ensures the
// in-memory and postgres implementations emit byte-identical cursors
// so a cursor minted by one backend is consumable by the other (and
// by handler tests).
package sharedcursor

import (
	"encoding/base64"
	"strings"
	"time"
)

// EncodeFollow produces the composite cursor used by
// FollowRepository.ListFollowers / ListFollowing:
//
//	base64url( RFC3339Nano(created_at) + "|" + peer_sub )
//
// The "|" separator cannot appear in either RFC3339Nano or a Crockford
// Base32 ULID, so splitting on the first "|" is unambiguous.
func EncodeFollow(t time.Time, peer string) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + peer
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeFollow reverses EncodeFollow. Returns ok=false for a nil /
// empty cursor, bad base64, a missing separator, or an unparseable
// time. Callers that want "start from the top" semantics on a
// malformed cursor can map ok=false to zero values.
func DecodeFollow(cursor *string) (time.Time, string, bool) {
	if cursor == nil || *cursor == "" {
		return time.Time{}, "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return time.Time{}, "", false
	}
	idx := strings.IndexByte(string(raw), '|')
	if idx <= 0 || idx == len(raw)-1 {
		return time.Time{}, "", false
	}
	t, err := time.Parse(time.RFC3339Nano, string(raw[:idx]))
	if err != nil {
		return time.Time{}, "", false
	}
	return t, string(raw[idx+1:]), true
}
