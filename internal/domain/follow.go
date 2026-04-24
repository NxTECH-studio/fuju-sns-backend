package domain

import "time"

// Follow is a directed relationship: FollowerSub follows FolloweeSub. Both
// subs are ULIDs owned by AuthCore.
type Follow struct {
	FollowerSub string
	FolloweeSub string
	CreatedAt   time.Time
}

// Validate rejects empty / self-directed follow rows. Existence of the
// counterparties is verified at the usecase layer against UserRepository.
func (f *Follow) Validate() error {
	if f.FollowerSub == "" || f.FolloweeSub == "" {
		return NewValidationError("follower_sub and followee_sub are required")
	}
	if f.FollowerSub == f.FolloweeSub {
		return NewValidationError("cannot follow yourself")
	}
	return nil
}
