package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

type emailSecurityStore struct {
	store.UserChannelStore
	mu                 sync.Mutex
	lookupErr, linkErr error
	linked             []*store.UserChannel
}

func (s *emailSecurityStore) GetByExternalID(context.Context, string, string) (*store.UserChannel, error) {
	return nil, s.lookupErr
}
func (s *emailSecurityStore) Link(_ context.Context, ch *store.UserChannel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.linkErr != nil {
		return s.linkErr
	}
	copy := *ch
	s.linked = append(s.linked, &copy)
	return nil
}

type failingEmailCodeStore struct {
	store.LinkCodeStore
	failPeek, failConsume bool
}

func (s failingEmailCodeStore) Peek(ctx context.Context, code string) (*store.LinkCode, error) {
	if s.failPeek {
		return nil, errors.New("lookup failed")
	}
	return s.LinkCodeStore.Peek(ctx, code)
}
func (s failingEmailCodeStore) Consume(ctx context.Context, code string) (*store.LinkCode, error) {
	if s.failConsume {
		return nil, errors.New("delete failed")
	}
	return s.LinkCodeStore.Consume(ctx, code)
}

func emailSecurityRequest(owner, code string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/user/channels/verify-email-code", strings.NewReader(`{"code":"`+code+`"}`))
	r = r.WithContext(WithPlatformUser(r.Context(), &PlatformUser{ID: owner}))
	w := httptest.NewRecorder()
	handleVerifyEmailCode(w, r)
	return w
}

func installEmailSecurityFixture(t *testing.T, codes LinkCodeBackend, target store.UserChannelStore) {
	t.Helper()
	oldCodes, oldBackend, oldAttempts := GetLinkCodeStore(), platformBackendInstance, channelLinkAttempts
	SetLinkCodeStore(codes)
	platformBackendInstance = &verifyUserChannelBackend{channels: target}
	channelLinkAttempts = NewLinkAttemptLimiter()
	t.Cleanup(func() {
		SetLinkCodeStore(oldCodes)
		platformBackendInstance = oldBackend
		channelLinkAttempts = oldAttempts
	})
}

func TestVerifyEmailCode_HTTPConcurrentSingleWinner(t *testing.T) {
	for _, kind := range []string{"memory", "database-adapter"} {
		t.Run(kind, func(t *testing.T) {
			var codes LinkCodeBackend = NewLinkCodeStore()
			if kind == "database-adapter" {
				codes = NewDBLinkCodeBackend(newMemLinkCodeStore())
			}
			target := &emailSecurityStore{}
			installEmailSecurityFixture(t, codes, target)
			code, err := codes.Generate(context.Background(), "owner", "a@example.com", "email:a@example.com")
			if err != nil {
				t.Fatal(err)
			}
			// A wrong owner must neither consume the proof nor create a link.
			if w := emailSecurityRequest("other", code); w.Code != http.StatusBadRequest {
				t.Fatalf("wrong owner: %d", w.Code)
			}
			if codes.Peek(context.Background(), code) == nil || len(target.linked) != 0 {
				t.Fatal("wrong owner consumed or persisted proof")
			}
			start := make(chan struct{})
			results := make(chan int, 8)
			var wg sync.WaitGroup
			for range 8 {
				wg.Add(1)
				go func() { defer wg.Done(); <-start; results <- emailSecurityRequest("owner", code).Code }()
			}
			close(start)
			wg.Wait()
			close(results)
			successes := 0
			for status := range results {
				switch status {
				case http.StatusOK:
					successes++
				case http.StatusBadRequest:
				default:
					t.Errorf("unexpected status=%d", status)
				}
			}
			if successes != 1 || len(target.linked) != 1 {
				t.Fatalf("successes=%d persisted=%d", successes, len(target.linked))
			}
			ch := target.linked[0]
			if ch.UserID != "owner" || !ch.Enabled || !ch.Verified || ch.ExternalID != "a@example.com" {
				t.Fatalf("incorrect link: %+v", ch)
			}
			if w := emailSecurityRequest("owner", code); w.Code != http.StatusBadRequest {
				t.Fatalf("replay status=%d", w.Code)
			}
		})
	}
}

func TestVerifyEmailCode_StorageFailuresFailClosed(t *testing.T) {
	for _, failure := range []string{"peek", "consume", "lookup", "link"} {
		t.Run(failure, func(t *testing.T) {
			backing := newMemLinkCodeStore()
			codes := NewDBLinkCodeBackend(failingEmailCodeStore{LinkCodeStore: backing, failPeek: failure == "peek", failConsume: failure == "consume"})
			target := &emailSecurityStore{}
			if failure == "lookup" {
				target.lookupErr = errors.New("database unavailable")
			}
			if failure == "link" {
				target.linkErr = errors.New("write failed")
			}
			installEmailSecurityFixture(t, codes, target)
			code, err := codes.Generate(context.Background(), "owner", "a@example.com", "email:a@example.com")
			if err != nil {
				t.Fatal(err)
			}
			want := http.StatusInternalServerError
			if failure == "peek" || failure == "consume" {
				want = http.StatusBadRequest
			}
			if w := emailSecurityRequest("owner", code); w.Code != want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			if len(target.linked) != 0 {
				t.Fatal("failed storage operation created link")
			}
			pending, err := backing.Peek(context.Background(), code)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "peek" || failure == "consume" {
				if pending == nil {
					t.Fatal("failed read/delete lost proof")
				}
			} else {
				if pending != nil {
					t.Fatal("proof reusable after persistence failure")
				}
				if w := emailSecurityRequest("owner", code); w.Code != http.StatusBadRequest {
					t.Fatalf("failed-write replay status=%d", w.Code)
				}
			}
		})
	}
}

func TestVerifyEmailCode_ExpiredProofNoPersistence(t *testing.T) {
	backing := newMemLinkCodeStore()
	codes := NewDBLinkCodeBackend(backing)
	target := &emailSecurityStore{}
	installEmailSecurityFixture(t, codes, target)
	if err := backing.GenerateWithTTL(context.Background(), "expired", "owner", "a@example.com", "email:a@example.com", -time.Second); err != nil {
		t.Fatal(err)
	}
	if w := emailSecurityRequest("owner", "expired"); w.Code != http.StatusBadRequest {
		t.Fatalf("expired status=%d", w.Code)
	}
	if len(target.linked) != 0 {
		t.Fatal("expired proof created link")
	}
}
