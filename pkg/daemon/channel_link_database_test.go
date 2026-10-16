package daemon

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/channels"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
	"github.com/google/uuid"
)

func TestChannelLinkDatabaseBindingSurvivesRestartAndSingleConsume(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := entstore.Config{DSN: "file:" + filepath.Join(dir, "platform.db"), DataDir: dir}
	_, db, err := entstore.NewPlatformServices(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := db.Users().Create(ctx, &store.User{ID: owner, Email: "owner@example.com", DisplayName: "Owner", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	binding, err := channels.LinkBinding(channels.LinkSlack, channels.LinkIdentity{WorkspaceID: "TDB123", UserID: "UDB123"})
	if err != nil {
		t.Fatal(err)
	}
	codes := api.NewDBLinkCodeBackend(db.LinkCodes())
	code, err := codes.Generate(ctx, owner, "owner@example.com", binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = entstore.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	codes = api.NewDBLinkCodeBackend(db.LinkCodes())
	handler := buildSlackLinkHandler(db, codes, nil)
	for _, principal := range []string{"TOTHER:UDB123", "TDB123:UOTHER"} {
		if ok, _ := handler(ctx, principal, "wrong", code); ok {
			t.Fatal("wrong identity accepted")
		}
		if codes.Peek(ctx, code) == nil {
			t.Fatal("wrong identity consumed persisted code")
		}
	}
	if ok, message := handler(ctx, "TDB123:UDB123", "owner", code); !ok {
		t.Fatal(message)
	}
	linked, err := db.UserChannels().GetByExternalID(ctx, "slack", "TDB123:UDB123")
	if err != nil || linked == nil || linked.UserID != owner || !linked.Verified {
		t.Fatalf("persisted link=%+v error=%v", linked, err)
	}
	// Exercise the real storage consume race, not just a mock or process mutex.
	code, err = codes.Generate(ctx, owner, "owner@example.com", binding)
	if err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if codes.Consume(ctx, code) != nil {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("consume winners=%d, want 1", winners.Load())
	}
}
