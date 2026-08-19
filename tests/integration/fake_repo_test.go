package integration

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/fireflg/gophprofile/internal/domain"
)

type fakeRepo struct {
	mu      sync.RWMutex
	avatars map[uuid.UUID]*domain.Avatar
}

var _ domain.AvatarRepository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{avatars: make(map[uuid.UUID]*domain.Avatar)}
}

func (r *fakeRepo) Create(_ context.Context, avatar *domain.Avatar) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	avatar.CreatedAt = nowUTC()
	avatar.UpdatedAt = avatar.CreatedAt

	stored := *avatar
	r.avatars[avatar.ID] = &stored

	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Avatar, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	avatar, ok := r.avatars[id]
	if !ok || avatar.DeletedAt != nil {
		return nil, domain.ErrAvatarNotFound
	}

	copied := *avatar

	return &copied, nil
}

func (r *fakeRepo) GetLatestByUser(ctx context.Context, userID string) (*domain.Avatar, error) {
	avatars, err := r.ListByUser(ctx, userID, 1, 0)
	if err != nil {
		return nil, err
	}

	if len(avatars) == 0 {
		return nil, domain.ErrAvatarNotFound
	}

	return avatars[0], nil
}

func (r *fakeRepo) ListByUser(_ context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	found := make([]*domain.Avatar, 0, len(r.avatars))
	for _, avatar := range r.avatars {
		if avatar.UserID == userID && avatar.DeletedAt == nil {
			copied := *avatar
			found = append(found, &copied)
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].CreatedAt.After(found[j].CreatedAt) })

	if offset >= len(found) {
		return nil, nil
	}

	found = found[offset:]
	if len(found) > limit {
		found = found[:limit]
	}

	return found, nil
}

func (r *fakeRepo) SetUploadStatus(_ context.Context, id uuid.UUID, status domain.UploadStatus) error {
	return r.update(id, func(avatar *domain.Avatar) { avatar.UploadStatus = status })
}

func (r *fakeRepo) SetProcessingStatus(_ context.Context, id uuid.UUID, status domain.ProcessingStatus) error {
	return r.update(id, func(avatar *domain.Avatar) { avatar.ProcessingStatus = status })
}

func (r *fakeRepo) SaveProcessingResult(
	_ context.Context,
	id uuid.UUID,
	thumbnails map[string]string,
	width, height int,
) error {
	return r.update(id, func(avatar *domain.Avatar) {
		avatar.ThumbnailS3Keys = thumbnails
		avatar.Width = width
		avatar.Height = height
		avatar.ProcessingStatus = domain.ProcessingStatusReady
	})
}

func (r *fakeRepo) SoftDelete(_ context.Context, id uuid.UUID) error {
	return r.update(id, func(avatar *domain.Avatar) {
		deletedAt := nowUTC()
		avatar.DeletedAt = &deletedAt
	})
}

func (r *fakeRepo) TotalStorageBytes(context.Context) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var total int64
	for _, avatar := range r.avatars {
		if avatar.DeletedAt == nil {
			total += avatar.SizeBytes
		}
	}

	return total, nil
}

func (r *fakeRepo) Ping(context.Context) error { return nil }

func (r *fakeRepo) update(id uuid.UUID, mutate func(*domain.Avatar)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	avatar, ok := r.avatars[id]
	if !ok || avatar.DeletedAt != nil {
		return domain.ErrAvatarNotFound
	}

	mutate(avatar)
	avatar.UpdatedAt = nowUTC()

	return nil
}
