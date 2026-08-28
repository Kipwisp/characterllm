package session

import (
	"context"
)

// imageCandidateStore keeps the in-memory avatar image candidate URL lists used during
// character creation.
type imageCandidateStore struct{ *core }

// SaveImageCandidates stores a list of candidate image URLs under an
// opaque key (the avatar menu's token) for the character setup process.
func (s *imageCandidateStore) SaveImageCandidates(ctx context.Context, key string, urls []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.imageCandidates[key] = urls
	return nil
}

// GetImageCandidates retrieves the candidate image URLs stored under key.
func (s *imageCandidateStore) GetImageCandidates(ctx context.Context, key string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	urls, ok := s.imageCandidates[key]
	if !ok {
		return nil, nil
	}
	return urls, nil
}

// ClearImageCandidates removes the candidate image URLs stored under key.
func (s *imageCandidateStore) ClearImageCandidates(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.imageCandidates, key)
	return nil
}
