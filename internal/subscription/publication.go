package subscription

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// ObjectTimeout bounds uploads, downloads, and cleanup requests.
	ObjectTimeout = 30 * time.Second
	stagingGrace  = 2 * time.Minute
	retiredGrace  = 30 * time.Second
)

var (
	ErrUploadFailed = errors.New("subscription upload failed")
	ErrNotPublished = errors.New("subscription is not published")
)

// Publication identifies the immutable YAML currently available to clients.
type Publication struct {
	ObjectKey    string    `json:"object_key"`
	DraftVersion uint64    `json:"draft_version"`
	Attempt      uint64    `json:"attempt"`
	PublishedAt  time.Time `json:"published_at"`
}

// DraftValidationError reports a validation problem before publication.
type DraftValidationError struct{ err error }

func (e DraftValidationError) Error() string { return e.err.Error() }

// Publisher manages the lifecycle of private YAML objects and published links.
type Publisher struct {
	store   *Store
	objects *S3Objects
}

func NewPublisher(store *Store, objects *S3Objects) *Publisher {
	return &Publisher{store: store, objects: objects}
}

// Publish validates and saves an editor submission, uploads its YAML, and changes
// the download only if the reserved draft and publication attempt are current.
func (p *Publisher) Publish(ctx context.Context, id string, version uint64, draft Draft) error {
	if err := draft.normalizeName(); err != nil {
		return err
	}
	yaml, err := GenerateYAML(draft)
	if err != nil {
		return DraftValidationError{err}
	}

	// Start the timeout before staging, so an upload cannot begin after
	// a paused publisher wakes up past its staging deadline.
	uploadCtx, cancel := context.WithTimeout(ctx, ObjectTimeout)
	defer cancel()
	key := p.objects.prefix + id + "/" + rand.Text() + ".yaml"
	candidate, err := p.store.stagePublication(uploadCtx, id, key, version, draft)
	if err != nil {
		return err
	}
	err = p.objects.put(uploadCtx, key, yaml)
	if err != nil {
		// A transport failure may outlive this PUT call. Leave the staged
		// object for cleanup after the settling period.
		return fmt.Errorf("%w: %w", ErrUploadFailed, err)
	}

	err = p.store.commitPublication(ctx, id, candidate)
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
		p.store.expediteCleanup(ctx, candidate.ObjectKey)
	}
	return err
}

// Download verifies the link and selects the object in the same Redis read.
func (p *Publisher) Download(ctx context.Context, id, token string) (io.ReadCloser, int64, error) {
	sub, err := p.store.Get(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if sub.Publication == nil || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(sub.DownloadToken)) != 1 {
		return nil, 0, ErrNotFound
	}
	return p.objects.get(ctx, sub.Publication.ObjectKey)
}

// ResetDownloadToken revokes the current link while keeping its publication.
func (s *Store) ResetDownloadToken(ctx context.Context, id string) error {
	return s.watch(ctx, []string{subscriptionKey(id)}, func(tx *redis.Tx) error {
		sub, err := readSubscription(ctx, tx, id)
		if err != nil {
			return err
		}
		if sub.Publication == nil {
			return ErrNotPublished
		}
		sub.DownloadToken = rand.Text()
		return writeSubscription(ctx, tx, sub, nil)
	})
}

func (s *Store) stagePublication(ctx context.Context, id, objectKey string, version uint64, draft Draft) (Publication, error) {
	recordKey := subscriptionKey(id)
	var candidate Publication
	err := s.watch(ctx, []string{recordKey}, func(tx *redis.Tx) error {
		sub, err := readSubscription(ctx, tx, id)
		if err != nil {
			return err
		}
		if sub.DraftVersion != version {
			return ErrConflict
		}
		sub.updateDraft(draft)
		sub.PublishAttempt++
		candidate = Publication{ObjectKey: objectKey, DraftVersion: sub.DraftVersion, Attempt: sub.PublishAttempt}
		return writeSubscription(ctx, tx, sub, func(pipe redis.Pipeliner) {
			queueCleanup(ctx, pipe, objectKey, cleanupStaged, time.Now().Add(stagingGrace))
		})
	})
	return candidate, err
}

func (s *Store) commitPublication(ctx context.Context, id string, candidate Publication) error {
	recordKey := subscriptionKey(id)
	jobKey := cleanupJobKey(candidate.ObjectKey)
	return s.watch(ctx, []string{recordKey, jobKey}, func(tx *redis.Tx) error {
		sub, err := readSubscription(ctx, tx, id)
		if err != nil {
			return err
		}
		if sub.DraftVersion != candidate.DraftVersion || (sub.Publication != nil && sub.Publication.Attempt >= candidate.Attempt) {
			return ErrConflict
		}
		state, err := tx.Get(ctx, jobKey).Result()
		if errors.Is(err, redis.Nil) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if state != cleanupStaged {
			return ErrConflict
		}
		previous := sub.Publication
		candidate.PublishedAt = time.Now().UTC()
		sub.Publication = &candidate
		if sub.DownloadToken == "" {
			sub.DownloadToken = rand.Text()
		}
		return writeSubscription(ctx, tx, sub, func(pipe redis.Pipeliner) {
			acknowledgeCleanup(ctx, pipe, candidate.ObjectKey)
			if previous != nil {
				queueCleanup(ctx, pipe, previous.ObjectKey, cleanupRetired, candidate.PublishedAt.Add(retiredGrace))
			}
		})
	})
}
