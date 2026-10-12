package live

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func joinCookies(t *testing.T, f *wsFixture, code, name string) *http.Cookie {
	t.Helper()
	req, _ := http.NewRequest("POST", f.url+"/api/v1/join/"+code, strings.NewReader(`{"displayName":"`+name+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("join: %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-qf_participant" {
			return c
		}
	}
	t.Fatal("no participant cookie")
	return nil
}

func readUntilType(t *testing.T, conn *websocket.Conn, want string, timeout time.Duration) map[string]any {
	t.Helper()
	// One long-lived read context: expiring a coder/websocket Read context
	// closes the connection, so never poll with short timeouts here.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("never saw %s: %v", want, err)
		}
		var env map[string]any
		_ = json.Unmarshal(raw, &env)
		if typ, _ := env["type"].(string); typ == want {
			return env
		}
	}
}

// TestPresenceLeave fires only after the grace window with no reconnect.
func TestPresenceLeave(t *testing.T) {
	f := setupWS(t)
	ctx := context.Background()
	sess, err := f.svc.StartSession(ctx, f.quiz.ID, f.host, true, true)
	if err != nil {
		t.Fatal(err)
	}
	hostConn := dialWS(t, f, sess.ID, f.hostCk)
	defer hostConn.Close(websocket.StatusNormalClosure, "done")
	_ = readEnv(t, hostConn) // snapshot
	// Drain any presence noise from our own dial (host dials don't emit presence).

	partCk := joinCookies(t, f, sess.JoinCode, "Leaver")
	partConn := dialWS(t, f, sess.ID, partCk)
	_ = readEnv(t, partConn) // snapshot
	// Host learns the join via count change.
	readUntilType(t, hostConn, "participant_count_changed", 5*time.Second)

	if err := partConn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatal(err)
	}
	env := readUntilType(t, hostConn, "participant_left", 12*time.Second)
	payload := env["payload"].(map[string]any)
	if _, ok := payload["participantId"]; !ok {
		t.Fatalf("payload: %v", payload)
	}
}

// TestPresenceReconnectCancelsLeave proves short interruptions are not departures.
func TestPresenceReconnectCancelsLeave(t *testing.T) {
	f := setupWS(t)
	ctx := context.Background()
	sess, err := f.svc.StartSession(ctx, f.quiz.ID, f.host, true, true)
	if err != nil {
		t.Fatal(err)
	}
	hostConn := dialWS(t, f, sess.ID, f.hostCk)
	defer hostConn.Close(websocket.StatusNormalClosure, "done")
	_ = readEnv(t, hostConn)

	partCk := joinCookies(t, f, sess.JoinCode, "Flicker")
	c1 := dialWS(t, f, sess.ID, partCk)
	_ = readEnv(t, c1)
	readUntilType(t, hostConn, "participant_count_changed", 5*time.Second)
	_ = c1.Close(websocket.StatusNormalClosure, "refresh")
	// Reconnect well inside the 5s grace window with the same resume cookie:
	// same identity (count unchanged), and no leave event follows.
	time.Sleep(time.Second)
	c2 := dialWS(t, f, sess.ID, partCk)
	defer c2.Close(websocket.StatusNormalClosure, "done")
	_ = readEnv(t, c2)

	sess2, err := f.svc.Store().GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess2.ParticipantCount != 1 {
		t.Fatalf("duplicate identity created: count=%d", sess2.ParticipantCount)
	}
	// No leave may arrive within the grace window + margin. Sleep without
	// reading (well under the 20s ping interval, so the connection stays
	// healthy), then do one bounded read: a timeout means nothing arrived.
	time.Sleep(7 * time.Second)
	wctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, raw, err := hostConn.Read(wctx)
	cancel()
	if err == nil {
		var env map[string]any
		_ = json.Unmarshal(raw, &env)
		if typ, _ := env["type"].(string); typ == "participant_left" {
			t.Fatal("leave emitted despite reconnect inside grace window")
		}
	}
	// (A read-context expiry here is fine: the test performs no further I/O.)
	_ = websocket.StatusNormalClosure
}
