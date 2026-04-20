// Package inmemory provides in-memory implementations of repositories for testing.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/oklog/ulid/v2"
)

// UserRepository is an in-memory implementation of the UserRepository interface.
type UserRepository struct {
	mu    sync.RWMutex
	users map[string]*domain.User
}

// NewUserRepository creates a new in-memory user repository.
func NewUserRepository() repository.UserRepository {
	return &UserRepository{
		users: make(map[string]*domain.User),
	}
}

// GetBySub retrieves a user by AuthCore sub.
func (r *UserRepository) GetBySub(_ context.Context, sub string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[sub]
	if !ok {
		return nil, nil
	}

	userCopy := *user
	return &userCopy, nil
}

// Upsert inserts or updates a user. On update, the stored values for
// fields the caller did not populate (zero values in the input) are
// preserved — so hydrate refreshing only *_Cached fields cannot accidentally
// clobber is_admin, bio, banner_url, or created_at.
func (r *UserRepository) Upsert(_ context.Context, user *domain.User) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	existing, ok := r.users[user.Sub]
	stored := *user

	if ok {
		// Preserved server-owned fields.
		stored.IsAdmin = existing.IsAdmin
		stored.CreatedAt = existing.CreatedAt
		stored.DeletedAt = existing.DeletedAt
		// Preserve SNS-owned fields when the caller did not overwrite them.
		if stored.Bio == "" {
			stored.Bio = existing.Bio
		}
		if stored.BannerURL == "" {
			stored.BannerURL = existing.BannerURL
		}
	} else if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now

	r.users[user.Sub] = &stored
	storedCopy := stored
	return &storedCopy, nil
}

// UpdateProfile updates SNS-owned fields (bio, banner_url).
func (r *UserRepository) UpdateProfile(_ context.Context, sub string, req *domain.UpdateUserProfileRequest) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.users[sub]
	if !ok {
		return nil, nil
	}

	if req.Bio != nil {
		user.Bio = *req.Bio
	}
	if req.BannerURL != nil {
		user.BannerURL = *req.BannerURL
	}
	user.UpdatedAt = time.Now()

	userCopy := *user
	return &userCopy, nil
}

// List retrieves a paginated list of users.
func (r *UserRepository) List(_ context.Context, limit, offset int) ([]*domain.User, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var users []*domain.User
	for _, user := range r.users {
		if user.DeletedAt == nil {
			userCopy := *user
			users = append(users, &userCopy)
		}
	}

	total := len(users)
	if offset > total {
		offset = total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return users[offset:end], total, nil
}

// Delete soft-deletes a user.
func (r *UserRepository) Delete(_ context.Context, sub string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, ok := r.users[sub]; ok {
		now := time.Now()
		user.DeletedAt = &now
	}

	return nil
}

// LinkStore backs many-to-many relationships (post_images, post_tags) that
// cross repository boundaries in the in-memory driver. The SQL model keeps
// each relation in its own table; in-memory we share a single
// mutex-guarded struct so PostRepository can attach links during Create
// and ImageRepository / TagRepository can read them back.
type LinkStore struct {
	mu         sync.RWMutex
	postImages map[string][]string            // post_id -> ordered image ids
	postTags   map[string]map[string]struct{} // post_id -> set of tag ids
}

// NewLinkStore constructs an empty LinkStore.
func NewLinkStore() *LinkStore {
	return &LinkStore{
		postImages: make(map[string][]string),
		postTags:   make(map[string]map[string]struct{}),
	}
}

func (s *LinkStore) attachImages(postID string, imageIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]string, len(imageIDs))
	copy(cp, imageIDs)
	s.postImages[postID] = cp
}

func (s *LinkStore) attachTags(postID string, tagIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(tagIDs) == 0 {
		return
	}
	set := s.postTags[postID]
	if set == nil {
		set = make(map[string]struct{})
		s.postTags[postID] = set
	}
	for _, id := range tagIDs {
		set[id] = struct{}{}
	}
}

func (s *LinkStore) imageIDsByPost(postID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.postImages[postID]
	cp := make([]string, len(ids))
	copy(cp, ids)
	return cp
}

func (s *LinkStore) tagIDsByPost(postID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.postTags[postID]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// PostRepository is an in-memory implementation of the PostRepository interface.
type PostRepository struct {
	mu    sync.RWMutex
	posts map[string]*domain.Post
	links *LinkStore
}

// NewPostRepository creates a new in-memory post repository.
func NewPostRepository(links *LinkStore) repository.PostRepository {
	return &PostRepository{
		posts: make(map[string]*domain.Post),
		links: links,
	}
}

// GetByID returns (nil, nil) for missing or soft-deleted posts.
func (r *PostRepository) GetByID(_ context.Context, id string) (*domain.Post, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	post, ok := r.posts[id]
	if !ok || post.DeletedAt != nil {
		return nil, nil
	}
	postCopy := *post
	return &postCopy, nil
}

// Create stores the post and attaches images / tags via the shared LinkStore.
func (r *PostRepository) Create(_ context.Context, post *domain.Post, imageIDs []string, tagIDs []string) (*domain.Post, error) {
	r.mu.Lock()
	now := time.Now()
	post.CreatedAt = now
	post.UpdatedAt = now

	postCopy := *post
	r.posts[post.ID] = &postCopy
	r.mu.Unlock()

	if len(imageIDs) > 0 {
		r.links.attachImages(post.ID, imageIDs)
	}
	if len(tagIDs) > 0 {
		r.links.attachTags(post.ID, tagIDs)
	}

	out := postCopy
	return &out, nil
}

// Delete soft-deletes a post.
func (r *PostRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if post, ok := r.posts[id]; ok {
		now := time.Now()
		post.DeletedAt = &now
	}
	return nil
}

// List returns the top `limit` posts (id DESC) optionally filtered by
// userID and bounded by cursor (exclusive upper bound on id).
func (r *PostRepository) List(_ context.Context, userID *string, cursor *string, limit int) ([]*domain.Post, string, error) {
	filter := func(p *domain.Post) bool {
		if p.DeletedAt != nil {
			return false
		}
		if userID != nil && p.UserID != *userID {
			return false
		}
		return p.ParentPostID == nil // top-level posts only on the main feed
	}
	posts := r.collectDescending(filter, cursor)
	return paginate(posts, limit)
}

// ListByUserIDs returns the union of posts from every author in userIDs.
func (r *PostRepository) ListByUserIDs(_ context.Context, userIDs []string, cursor *string, limit int) ([]*domain.Post, string, error) {
	wanted := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		wanted[id] = struct{}{}
	}
	filter := func(p *domain.Post) bool {
		if p.DeletedAt != nil {
			return false
		}
		if _, ok := wanted[p.UserID]; !ok {
			return false
		}
		return p.ParentPostID == nil
	}
	posts := r.collectDescending(filter, cursor)
	return paginate(posts, limit)
}

// ListReplies returns direct replies of postID (parent_post_id == postID).
func (r *PostRepository) ListReplies(_ context.Context, postID string, cursor *string, limit int) ([]*domain.Post, string, error) {
	filter := func(p *domain.Post) bool {
		if p.DeletedAt != nil {
			return false
		}
		return p.ParentPostID != nil && *p.ParentPostID == postID
	}
	posts := r.collectDescending(filter, cursor)
	return paginate(posts, limit)
}

// collectDescending walks the posts map, filters, and returns matches in
// id-DESC order. `cursor`, if set, is an exclusive upper bound on id
// (only posts with id < cursor are returned).
func (r *PostRepository) collectDescending(filter func(*domain.Post) bool, cursor *string) []*domain.Post {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var posts []*domain.Post
	for _, p := range r.posts {
		if !filter(p) {
			continue
		}
		if cursor != nil && p.ID >= *cursor {
			continue
		}
		pc := *p
		posts = append(posts, &pc)
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].ID > posts[j].ID })
	return posts
}

// paginate slices posts to `limit` and returns the next cursor (the last
// item's ID) if there are more rows waiting, or "" otherwise.
func paginate(posts []*domain.Post, limit int) ([]*domain.Post, string, error) {
	if limit <= 0 {
		return nil, "", nil
	}
	if len(posts) <= limit {
		return posts, "", nil
	}
	page := posts[:limit]
	return page, page[limit-1].ID, nil
}

// IncrementRepliesCount bumps a counter. No-op for missing posts.
func (r *PostRepository) IncrementRepliesCount(_ context.Context, postID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.posts[postID]; ok {
		p.RepliesCount++
	}
	return nil
}

// DecrementRepliesCount decrements a counter, flooring at 0.
func (r *PostRepository) DecrementRepliesCount(_ context.Context, postID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.posts[postID]; ok && p.RepliesCount > 0 {
		p.RepliesCount--
	}
	return nil
}

// IncrementLikesCount bumps a counter.
func (r *PostRepository) IncrementLikesCount(_ context.Context, postID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.posts[postID]; ok {
		p.LikesCount++
	}
	return nil
}

// DecrementLikesCount decrements a counter, flooring at 0.
func (r *PostRepository) DecrementLikesCount(_ context.Context, postID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.posts[postID]; ok && p.LikesCount > 0 {
		p.LikesCount--
	}
	return nil
}

// ImageRepository is an in-memory implementation of the ImageRepository interface.
type ImageRepository struct {
	mu     sync.RWMutex
	images map[string]*domain.Image
	links  *LinkStore
}

// NewImageRepository creates a new in-memory image repository.
func NewImageRepository(links *LinkStore) repository.ImageRepository {
	return &ImageRepository{
		images: make(map[string]*domain.Image),
		links:  links,
	}
}

// GetByID retrieves an image by ID.
func (r *ImageRepository) GetByID(_ context.Context, id string) (*domain.Image, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	image, ok := r.images[id]
	if !ok || image.DeletedAt != nil {
		return nil, nil
	}
	imageCopy := *image
	return &imageCopy, nil
}

// GetByUserID retrieves all images for a user.
func (r *ImageRepository) GetByUserID(_ context.Context, userID string) ([]*domain.Image, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var images []*domain.Image
	for _, image := range r.images {
		if image.UserID == userID && image.DeletedAt == nil {
			imageCopy := *image
			images = append(images, &imageCopy)
		}
	}
	return images, nil
}

// Create stores a new image record.
func (r *ImageRepository) Create(_ context.Context, image *domain.Image) (*domain.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	image.CreatedAt = now
	image.UpdatedAt = now

	imageCopy := *image
	r.images[image.ID] = &imageCopy
	return &imageCopy, nil
}

// Delete soft-deletes an image.
func (r *ImageRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if image, ok := r.images[id]; ok {
		now := time.Now()
		image.DeletedAt = &now
	}
	return nil
}

// ListByPostID returns the images attached to postID in position order.
func (r *ImageRepository) ListByPostID(_ context.Context, postID string) ([]*domain.Image, error) {
	ids := r.links.imageIDsByPost(postID)
	if len(ids) == 0 {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.Image, 0, len(ids))
	for _, id := range ids {
		img, ok := r.images[id]
		if !ok || img.DeletedAt != nil {
			continue
		}
		ic := *img
		out = append(out, &ic)
	}
	return out, nil
}

// ListByPostIDs batches ListByPostID across many posts.
func (r *ImageRepository) ListByPostIDs(_ context.Context, postIDs []string) (map[string][]*domain.Image, error) {
	out := make(map[string][]*domain.Image, len(postIDs))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, pid := range postIDs {
		ids := r.links.imageIDsByPost(pid)
		if len(ids) == 0 {
			continue
		}
		imgs := make([]*domain.Image, 0, len(ids))
		for _, id := range ids {
			img, ok := r.images[id]
			if !ok || img.DeletedAt != nil {
				continue
			}
			ic := *img
			imgs = append(imgs, &ic)
		}
		if len(imgs) > 0 {
			out[pid] = imgs
		}
	}
	return out, nil
}

// LikeRepository is an in-memory implementation of LikeRepository.
type LikeRepository struct {
	mu    sync.RWMutex
	likes map[string]map[string]time.Time // userID -> postID -> created_at
}

// NewLikeRepository creates a new in-memory like repository.
func NewLikeRepository() repository.LikeRepository {
	return &LikeRepository{likes: make(map[string]map[string]time.Time)}
}

// Create inserts a like row. Returns (true, nil) when the row was newly
// inserted, (false, nil) when it already existed (idempotent).
func (r *LikeRepository) Create(_ context.Context, userID, postID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	byUser, ok := r.likes[userID]
	if !ok {
		byUser = make(map[string]time.Time)
		r.likes[userID] = byUser
	}
	if _, exists := byUser[postID]; exists {
		return false, nil
	}
	byUser[postID] = time.Now()
	return true, nil
}

// Delete removes a like row. Returns (true, nil) when a row was removed,
// (false, nil) when no row existed (idempotent).
func (r *LikeRepository) Delete(_ context.Context, userID, postID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	byUser, ok := r.likes[userID]
	if !ok {
		return false, nil
	}
	if _, exists := byUser[postID]; !exists {
		return false, nil
	}
	delete(byUser, postID)
	return true, nil
}

// IsLikedBy reports whether userID has liked postID.
func (r *LikeRepository) IsLikedBy(_ context.Context, userID, postID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byUser, ok := r.likes[userID]
	if !ok {
		return false, nil
	}
	_, exists := byUser[postID]
	return exists, nil
}

// ListLikedPostIDsByUser returns a map postID -> true for each post the
// user has liked, restricted to the input postIDs set.
func (r *LikeRepository) ListLikedPostIDsByUser(_ context.Context, userID string, postIDs []string) (map[string]bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(postIDs))
	byUser, ok := r.likes[userID]
	if !ok {
		return out, nil
	}
	for _, pid := range postIDs {
		if _, exists := byUser[pid]; exists {
			out[pid] = true
		}
	}
	return out, nil
}

// TagRepository is an in-memory implementation of TagRepository.
type TagRepository struct {
	mu     sync.RWMutex
	tags   map[string]*domain.Tag // id -> tag
	byName map[string]string      // name -> id
	links  *LinkStore
}

// NewTagRepository creates a new in-memory tag repository.
func NewTagRepository(links *LinkStore) repository.TagRepository {
	return &TagRepository{
		tags:   make(map[string]*domain.Tag),
		byName: make(map[string]string),
		links:  links,
	}
}

// UpsertByNames inserts any missing tag and returns the full set in the
// order of the input (after de-duplicating repeated names).
func (r *TagRepository) UpsertByNames(_ context.Context, names []string) ([]*domain.Tag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	seen := make(map[string]struct{}, len(names))
	out := make([]*domain.Tag, 0, len(names))
	now := time.Now()
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}

		if id, ok := r.byName[name]; ok {
			tc := *r.tags[id]
			out = append(out, &tc)
			continue
		}
		tag := &domain.Tag{
			ID:        ulid.Make().String(),
			Name:      name,
			CreatedAt: now,
		}
		r.tags[tag.ID] = tag
		r.byName[name] = tag.ID
		tc := *tag
		out = append(out, &tc)
	}
	return out, nil
}

// ListByPostID returns tags attached to postID.
func (r *TagRepository) ListByPostID(_ context.Context, postID string) ([]*domain.Tag, error) {
	ids := r.links.tagIDsByPost(postID)
	if len(ids) == 0 {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.Tag, 0, len(ids))
	for _, id := range ids {
		if t, ok := r.tags[id]; ok {
			tc := *t
			out = append(out, &tc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ListByPostIDs batches ListByPostID across posts.
func (r *TagRepository) ListByPostIDs(_ context.Context, postIDs []string) (map[string][]*domain.Tag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string][]*domain.Tag, len(postIDs))
	for _, pid := range postIDs {
		ids := r.links.tagIDsByPost(pid)
		if len(ids) == 0 {
			continue
		}
		tags := make([]*domain.Tag, 0, len(ids))
		for _, id := range ids {
			if t, ok := r.tags[id]; ok {
				tc := *t
				tags = append(tags, &tc)
			}
		}
		sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
		if len(tags) > 0 {
			out[pid] = tags
		}
	}
	return out, nil
}

// userBadgeKey is the composite primary key for the in-memory user_badges map.
type userBadgeKey struct {
	userID  string
	badgeID string
}

// BadgeRepository is an in-memory implementation of the BadgeRepository
// interface.
type BadgeRepository struct {
	mu         sync.RWMutex
	badges     map[string]*domain.Badge // id -> badge
	byKey      map[string]string        // key -> id
	userBadges map[userBadgeKey]*domain.UserBadge
}

// NewBadgeRepository creates a new in-memory badge repository.
func NewBadgeRepository() repository.BadgeRepository {
	return &BadgeRepository{
		badges:     make(map[string]*domain.Badge),
		byKey:      make(map[string]string),
		userBadges: make(map[userBadgeKey]*domain.UserBadge),
	}
}

// ListAll returns all badge master rows ordered by priority ascending.
func (r *BadgeRepository) ListAll(_ context.Context) ([]*domain.Badge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	badges := make([]*domain.Badge, 0, len(r.badges))
	for _, b := range r.badges {
		bc := *b
		badges = append(badges, &bc)
	}
	sortBadgesByPriority(badges)
	return badges, nil
}

// GetByKey looks up a badge by its machine-readable key.
func (r *BadgeRepository) GetByKey(_ context.Context, key string) (*domain.Badge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.byKey[key]
	if !ok {
		return nil, nil
	}
	b := *r.badges[id]
	return &b, nil
}

// GetByID looks up a badge by ULID.
func (r *BadgeRepository) GetByID(_ context.Context, id string) (*domain.Badge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	b, ok := r.badges[id]
	if !ok {
		return nil, nil
	}
	bc := *b
	return &bc, nil
}

// Create inserts a new badge master row. Caller is responsible for setting
// badge.ID (typically a ULID). Key must be unique.
func (r *BadgeRepository) Create(_ context.Context, badge *domain.Badge) (*domain.Badge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.byKey[badge.Key]; dup {
		return nil, nil
	}

	now := time.Now()
	stored := *badge
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now
	r.badges[stored.ID] = &stored
	r.byKey[stored.Key] = stored.ID

	out := stored
	return &out, nil
}

// Update replaces the mutable fields of an existing badge master row. Key
// cannot be changed through Update — callers that need to rename a key must
// delete + recreate.
func (r *BadgeRepository) Update(_ context.Context, badge *domain.Badge) (*domain.Badge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.badges[badge.ID]
	if !ok {
		return nil, nil
	}

	stored := *existing
	stored.Label = badge.Label
	stored.Description = badge.Description
	stored.IconURL = badge.IconURL
	stored.Color = badge.Color
	stored.Priority = badge.Priority
	stored.UpdatedAt = time.Now()

	r.badges[stored.ID] = &stored
	out := stored
	return &out, nil
}

// Grant upserts a user_badges row (idempotent for MVP).
func (r *BadgeRepository) Grant(_ context.Context, userID, badgeID, grantedBy string, expiresAt *time.Time, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var expiresCopy *time.Time
	if expiresAt != nil {
		v := *expiresAt
		expiresCopy = &v
	}

	r.userBadges[userBadgeKey{userID: userID, badgeID: badgeID}] = &domain.UserBadge{
		UserID:    userID,
		BadgeID:   badgeID,
		GrantedAt: time.Now(),
		GrantedBy: grantedBy,
		ExpiresAt: expiresCopy,
		Reason:    reason,
	}
	return nil
}

// Revoke removes a user_badges row. Revoking a missing grant is a no-op.
func (r *BadgeRepository) Revoke(_ context.Context, userID, badgeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.userBadges, userBadgeKey{userID: userID, badgeID: badgeID})
	return nil
}

// ListByUserID returns all currently-active badges for a user, priority asc.
func (r *BadgeRepository) ListByUserID(_ context.Context, userID string) ([]*domain.Badge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var result []*domain.Badge
	for k, ub := range r.userBadges {
		if k.userID != userID {
			continue
		}
		if ub.ExpiresAt != nil && !ub.ExpiresAt.After(now) {
			continue
		}
		if b, ok := r.badges[k.badgeID]; ok {
			bc := *b
			result = append(result, &bc)
		}
	}
	sortBadgesByPriority(result)
	return result, nil
}

// ListByUserIDs batches ListByUserID across many users and returns a map
// keyed by user ID. Users with no active badges are absent from the map.
func (r *BadgeRepository) ListByUserIDs(_ context.Context, userIDs []string) (map[string][]*domain.Badge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	want := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		want[id] = struct{}{}
	}

	now := time.Now()
	out := make(map[string][]*domain.Badge)
	for k, ub := range r.userBadges {
		if _, ok := want[k.userID]; !ok {
			continue
		}
		if ub.ExpiresAt != nil && !ub.ExpiresAt.After(now) {
			continue
		}
		if b, ok := r.badges[k.badgeID]; ok {
			bc := *b
			out[k.userID] = append(out[k.userID], &bc)
		}
	}
	for uid := range out {
		sortBadgesByPriority(out[uid])
	}
	return out, nil
}

func sortBadgesByPriority(badges []*domain.Badge) {
	sort.SliceStable(badges, func(i, j int) bool {
		if badges[i].Priority != badges[j].Priority {
			return badges[i].Priority < badges[j].Priority
		}
		return badges[i].Key < badges[j].Key
	})
}
