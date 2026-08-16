package integration

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/fireflg/gophprofile/internal/domain"
)

type fakeStorage struct {
	mu      sync.RWMutex
	objects map[string]fakeObject
	baseURL string
}

type fakeObject struct {
	data        []byte
	contentType string
	etag        string
	modifiedAt  time.Time
}

var _ domain.FileStorage = (*fakeStorage)(nil)

func newFakeStorage(baseURL string) *fakeStorage {
	return &fakeStorage{objects: make(map[string]fakeObject), baseURL: baseURL}
}

func (s *fakeStorage) Put(_ context.Context, key string, r io.Reader, _ int64, contentType string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	sum := md5.Sum(data)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.objects[key] = fakeObject{
		data:        data,
		contentType: contentType,
		etag:        hex.EncodeToString(sum[:]),
		modifiedAt:  nowUTC(),
	}

	return nil
}

func (s *fakeStorage) Get(_ context.Context, key string) (*domain.Object, error) {
	s.mu.RLock()
	object, ok := s.objects[key]
	s.mu.RUnlock()

	if !ok {
		return nil, domain.ErrAvatarNotFound
	}

	return &domain.Object{
		Body:         io.NopCloser(bytes.NewReader(object.data)),
		ContentType:  object.contentType,
		Size:         int64(len(object.data)),
		ETag:         object.etag,
		LastModified: object.modifiedAt,
	}, nil
}

func (s *fakeStorage) Delete(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, key := range keys {
		delete(s.objects, key)
	}

	return nil
}

func (s *fakeStorage) URL(key string) string {
	if key == "" {
		return ""
	}

	return s.baseURL + "/" + key
}

func (s *fakeStorage) Ping(_ context.Context) error { return nil }
