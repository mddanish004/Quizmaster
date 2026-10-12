package live

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestTimerCloseIdempotent proves the deadline path closes exactly once:
// overdue questions close on first call; repeats and pre-deadline calls are no-ops.
func TestTimerCloseIdempotent(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, err := svc.StartSession(ctx, q.ID, host, true, true)
	if err != nil {
		t.Fatal(err)
	}
	pos := 0
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "start_question", 1, &pos); err != nil {
		t.Fatal(err)
	}
	// Not yet due: no close.
	svc.TimerClose(ctx, sess.ID)
	fresh, _ := svc.Store().GetSession(ctx, sess.ID)
	if fresh.Status != StateQuestionOpen {
		t.Fatalf("early close: %s", fresh.Status)
	}
	// Force the deadline into the past (simulating crash + restart delay).
	if _, err := svc.Store().Pool().Exec(ctx, `UPDATE live_session SET question_ends_at=now()-interval '1 second' WHERE id=$1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	svc.TimerClose(ctx, sess.ID)
	closed, _ := svc.Store().GetSession(ctx, sess.ID)
	if closed.Status != StateQuestionResults {
		t.Fatalf("not closed: %s", closed.Status)
	}
	v := closed.StateVersion
	// Repeat: still exactly one close (version unchanged, no new event).
	svc.TimerClose(ctx, sess.ID)
	again, _ := svc.Store().GetSession(ctx, sess.ID)
	if again.StateVersion != v || again.Status != StateQuestionResults {
		t.Fatalf("double close: %+v", again)
	}
	var events int
	if err := svc.Store().Pool().QueryRow(ctx, `SELECT count(*) FROM session_event WHERE live_session_id=$1 AND event_type='question_ended'`, sess.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("question_ended events=%d, want 1", events)
	}
}

// TestRestartRecoveryRebuildsActors simulates a process restart: a fresh
// Manager must pick up the open overdue question and close it once.
func TestRestartRecoveryRebuildsActors(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, _ := svc.StartSession(ctx, q.ID, host, true, true)
	pos := 0
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "start_question", 1, &pos); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store().Pool().Exec(ctx, `UPDATE live_session SET question_ends_at=now()-interval '1 second' WHERE id=$1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	// New manager = new process. Recovery runs async; poll for the close.
	m2 := NewManager(svc, []string{})
	defer func() {
		// Retire everything the recovery created.
		m2.RetireSession(sess.ID)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		fresh, _ := svc.Store().GetSession(ctx, sess.ID)
		if fresh.Status == StateQuestionResults {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovery never closed: %s", fresh.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n := m2.ActorCount(); n < 1 {
		t.Fatalf("recovery created no actors: %d", n)
	}
}

// TestRetireSession unit-covers actor/timer/client cleanup without sockets.
func TestRetireSession(t *testing.T) {
	svc, _, _ := testSetup(t)
	mgr := svc.manager
	id := uuid.New()
	mgr.EnsureActor(id)
	mgr.ScheduleClose(id, time.Now().Add(time.Hour))
	if n := mgr.ActorCount(); n < 1 {
		t.Fatalf("actors=%d", n)
	}
	mgr.RetireSession(id)
	if n := mgr.ActorCount(); n != 0 {
		t.Fatalf("actors=%d after retire", n)
	}
	// Idempotent.
	mgr.RetireSession(id)
}

// TestActorRetiredOnEnd bounds manager memory after terminal sessions.
func TestActorRetiredOnEnd(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	mgr := svc.manager
	sess, _ := svc.StartSession(ctx, q.ID, host, true, true)
	if n := mgr.ActorCount(); n != 1 {
		t.Fatalf("actors=%d, want 1", n)
	}
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "end_session", 1, nil); err != nil {
		t.Fatal(err)
	}
	// Retirement happens after the 5s end grace; poll briefly.
	deadline := time.Now().Add(10 * time.Second)
	for mgr.ActorCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("actor not retired: %d", mgr.ActorCount())
		}
		time.Sleep(100 * time.Millisecond)
	}
}
