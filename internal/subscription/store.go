// Package subscription stores independent Mihomo subscription drafts.
package subscription

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

const (
	catalogKey = "clash-gen:{subscriptions}:catalog"
	nameLimit  = 120
	watchTries = 4
)

var (
	ErrNotFound = errors.New("subscription not found")
	ErrConflict = errors.New("draft has changed")
	ErrName     = errors.New("subscription name must be 1 to 120 characters")
)

// Draft is the editable configuration of one subscription.
type Draft struct {
	Name        string   `json:"name"`
	Nodes       []Node   `json:"nodes,omitempty"`
	Rules       []Rule   `json:"rules,omitempty"`
	MatchTarget string   `json:"match_target,omitempty"`
	Settings    Settings `json:"settings,omitzero"`
}

// Subscription contains a draft and the revision used for optimistic writes.
type Subscription struct {
	ID             string       `json:"id"`
	DraftVersion   uint64       `json:"draft_version"`
	Draft          Draft        `json:"draft"`
	DownloadToken  string       `json:"download_token,omitempty"`
	PublishAttempt uint64       `json:"publish_attempt,omitempty"`
	Publication    *Publication `json:"publication,omitempty"`
}

// Store persists subscriptions in Redis.
type Store struct {
	redis redis.UniversalClient
}

func NewStore(client redis.UniversalClient) *Store {
	return &Store{redis: client}
}

func (s *Store) Create(ctx context.Context, name string) (Subscription, error) {
	draft := Draft{Name: name}
	if err := draft.normalizeName(); err != nil {
		return Subscription{}, err
	}
	sub := Subscription{ID: rand.Text(), DraftVersion: 1, Draft: draft}
	err := writeSubscription(ctx, s.redis, sub, func(pipe redis.Pipeliner) {
		pipe.SAdd(ctx, catalogKey, sub.ID)
	})
	return sub, err
}

func (s *Store) Get(ctx context.Context, id string) (Subscription, error) {
	return readSubscription(ctx, s.redis, id)
}

func (s *Store) List(ctx context.Context) ([]Subscription, error) {
	ids, err := s.redis.SMembers(ctx, catalogKey).Result()
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	keys := make([]string, len(ids))
	for index, id := range ids {
		keys[index] = subscriptionKey(id)
	}
	records, err := s.redis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	subscriptions := make([]Subscription, 0, len(records))
	for _, record := range records {
		if record == nil { // A subscription can be deleted while the catalogue is read.
			continue
		}
		var sub Subscription
		if err := json.Unmarshal([]byte(record.(string)), &sub); err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, sub)
	}
	slices.SortFunc(subscriptions, func(a, b Subscription) int {
		if order := strings.Compare(a.Draft.Name, b.Draft.Name); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return subscriptions, nil
}

func (s *Store) SaveDraft(ctx context.Context, id string, version uint64, draft Draft) error {
	if err := draft.normalizeName(); err != nil {
		return err
	}
	return s.watch(ctx, []string{subscriptionKey(id)}, func(tx *redis.Tx) error {
		sub, err := readSubscription(ctx, tx, id)
		if err != nil {
			return err
		}
		if sub.DraftVersion != version {
			return ErrConflict
		}
		if !sub.updateDraft(draft) {
			return nil
		}
		return writeSubscription(ctx, tx, sub, nil)
	})
}

func (s *Store) watch(ctx context.Context, keys []string, update func(*redis.Tx) error) error {
	for range watchTries {
		err := s.redis.Watch(ctx, update, keys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return ErrConflict
}

func (s *Subscription) updateDraft(draft Draft) bool {
	if reflect.DeepEqual(s.Draft, draft) {
		return false
	}
	s.Draft = draft
	s.DraftVersion++
	return true
}

func writeSubscription(ctx context.Context, redisWriter redis.Cmdable, sub Subscription, update func(redis.Pipeliner)) error {
	raw, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	_, err = redisWriter.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, subscriptionKey(sub.ID), raw, 0)
		if update != nil {
			update(pipe)
		}
		return nil
	})
	return err
}

func (d *Draft) normalizeName() error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || utf8.RuneCountInString(d.Name) > nameLimit {
		return ErrName
	}
	return nil
}

func subscriptionKey(id string) string { return "clash-gen:{subscriptions}:record:" + id }

func readSubscription(ctx context.Context, redisReader redis.Cmdable, id string) (Subscription, error) {
	raw, err := redisReader.Get(ctx, subscriptionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, err
	}
	var sub Subscription
	err = json.Unmarshal(raw, &sub)
	return sub, err
}
