package entstore

import (
	"context"
	adksession "google.golang.org/adk/session"
	"testing"
)

func TestTeamSessionGetEnforcesSuppliedOwner(t *testing.T) {
	s := &teamSessionStore{client: newTeamClient(t)}
	ctx := context.Background()
	owner := "11111111-1111-1111-1111-111111111111"
	_, err := s.Create(ctx, &adksession.CreateRequest{AppName: "astonish", UserID: owner, SessionID: "owner-bound"})
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{owner, "", "22222222-2222-2222-2222-222222222222", "invalid"} {
		_, err := s.Get(ctx, &adksession.GetRequest{SessionID: "owner-bound", UserID: uid})
		allowed := uid == owner || uid == ""
		if (err == nil) != allowed {
			t.Fatalf("user %q allowed=%v err=%v", uid, allowed, err)
		}
	}
}
