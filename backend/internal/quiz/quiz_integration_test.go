package quiz

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/testutil"
)

func testService(t *testing.T) (*Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURLFor(t, "QUIZ"))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	ownerA, ownerB := uuid.New(), uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'a@test.local','owner_a','Owner A')`, ownerA); err != nil {
		t.Fatalf("seed user A: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'b@test.local','owner_b','Owner B')`, ownerB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}
	return NewService(pool), ownerA, ownerB
}

func TestCRUDLifecycle(t *testing.T) {
	svc, ownerA, _ := testService(t)
	ctx := context.Background()
	q, err := svc.CreateBlank(ctx, ownerA, "Go Basics", "warm-up")
	if err != nil {
		t.Fatal(err)
	}
	if q.Version != 1 || q.Status != "draft" {
		t.Fatalf("blank: %+v", q)
	}
	// Empty publish must fail.
	if _, err := svc.Publish(ctx, q.ID, ownerA, q.Version); err == nil {
		t.Fatal("empty publish accepted")
	}
	// Add two questions.
	q1 := validQuestion(0)
	q2 := validQuestion(1)
	updated, err := svc.Update(ctx, q.ID, ownerA, q.Version, "Go Basics", "warm-up", []Question{q1, q2})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != 2 || len(updated.Questions) != 2 {
		t.Fatalf("update: %+v", updated)
	}
	// Reload preserves order.
	reloaded, err := svc.GetForOwner(ctx, q.ID, ownerA)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Questions) != 2 || reloaded.Questions[0].Position != 0 || reloaded.Questions[1].Position != 1 {
		t.Fatal("order not preserved")
	}
	// Publish valid.
	pub, err := svc.Publish(ctx, q.ID, ownerA, updated.Version)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.Status != "published" {
		t.Fatalf("status: %s", pub.Status)
	}
	// Republish idempotent.
	if _, err := svc.Publish(ctx, q.ID, ownerA, pub.Version); err != nil {
		t.Fatalf("idempotent publish: %v", err)
	}
	// Editing published returns to draft.
	upd2, err := svc.Update(ctx, q.ID, ownerA, pub.Version, "Go Basics v2", "", []Question{q1, q2})
	if err != nil {
		t.Fatal(err)
	}
	if upd2.Status != "draft" {
		t.Fatalf("expected draft after edit, got %s", upd2.Status)
	}
	// Unpublish explicit.
	if _, err := svc.Publish(ctx, q.ID, ownerA, upd2.Version); err != nil {
		t.Fatal(err)
	}
	// Delete then get → 404.
	latest, _ := svc.GetForOwner(ctx, q.ID, ownerA)
	if err := svc.Delete(ctx, q.ID, ownerA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetForOwner(ctx, q.ID, ownerA); err != ErrNotFound {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	_ = latest
}

func TestOwnershipIsolation(t *testing.T) {
	svc, ownerA, ownerB := testService(t)
	ctx := context.Background()
	q, _ := svc.CreateBlank(ctx, ownerA, "Secret", "")
	if _, err := svc.GetForOwner(ctx, q.ID, ownerB); err != ErrNotFound {
		t.Fatalf("B read A's quiz: %v", err)
	}
	if _, err := svc.Update(ctx, q.ID, ownerB, 1, "Hijack", "", nil); err != ErrNotFound {
		t.Fatalf("B wrote A's quiz: %v", err)
	}
	if err := svc.Delete(ctx, q.ID, ownerB); err != ErrNotFound {
		t.Fatalf("B deleted A's quiz: %v", err)
	}
	if _, err := svc.Publish(ctx, q.ID, ownerB, 1); err != ErrNotFound {
		t.Fatalf("B published A's quiz: %v", err)
	}
	// B cannot infer existence via preview either.
	if _, err := svc.Preview(ctx, q.ID, ownerB); err != ErrNotFound {
		t.Fatalf("B previewed A's quiz: %v", err)
	}
}

func TestVersionConflict(t *testing.T) {
	svc, ownerA, _ := testService(t)
	ctx := context.Background()
	q, _ := svc.CreateBlank(ctx, ownerA, "V", "")
	if _, err := svc.Update(ctx, q.ID, ownerA, 999, "V2", "", nil); err == nil {
		t.Fatal("stale update accepted")
	} else {
		var vc *ErrVersionConflict
		if !errors.As(err, &vc) || vc.CurrentVersion != 1 {
			t.Fatalf("expected conflict with current=1, got %v", err)
		}
	}
}

func TestConcurrentVersionUpdate(t *testing.T) {
	svc, ownerA, _ := testService(t)
	ctx := context.Background()
	q, _ := svc.CreateBlank(ctx, ownerA, "Race", "")
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Update(ctx, q.ID, ownerA, 1, "Race", "", nil)
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, e := range errs {
		if e == nil {
			ok++
		} else {
			var vc *ErrVersionConflict
			if errors.As(e, &vc) {
				conflict++
			} else {
				t.Fatalf("unexpected error: %v", e)
			}
		}
	}
	if ok != 1 || conflict != 3 {
		t.Fatalf("expected exactly one winner: ok=%d conflict=%d", ok, conflict)
	}
}

func TestReorderConstraints(t *testing.T) {
	svc, ownerA, _ := testService(t)
	ctx := context.Background()
	q, _ := svc.CreateBlank(ctx, ownerA, "Order", "")
	a := validQuestion(0)
	b := validQuestion(1)
	// Gap in positions rejected.
	bad := []Question{a, b}
	bad[1].Position = 5
	if _, err := svc.Update(ctx, q.ID, ownerA, 1, "Order", "", bad); err == nil {
		t.Fatal("gapped positions accepted")
	}
	// Swap is allowed when positions are rewritten contiguously.
	a.Position, b.Position = 1, 0
	if _, err := svc.Update(ctx, q.ID, ownerA, 1, "Order", "", []Question{b, a}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
}

func TestPreviewNoWrites(t *testing.T) {
	svc, ownerA, _ := testService(t)
	ctx := context.Background()
	q, _ := svc.CreateBlank(ctx, ownerA, "Prev", "")
	before, _ := svc.GetForOwner(ctx, q.ID, ownerA)
	pv, err := svc.Preview(ctx, q.ID, ownerA)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := svc.GetForOwner(ctx, q.ID, ownerA)
	if before.Version != after.Version || pv.Version != before.Version {
		t.Fatal("preview mutated version")
	}
}
