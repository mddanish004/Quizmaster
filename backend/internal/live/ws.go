package live

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Protocol version and limits (REALTIME_DESIGN.md).
const (
	ProtocolVersion = 1
	MaxMessageBytes = 16384
	OutboundQueue   = 64
	PingInterval    = 20 * time.Second
	PongTimeout     = 10 * time.Second
)

// Envelope is the shared wire shape for commands and events.
type Envelope struct {
	V            int            `json:"v"`
	Type         string         `json:"type"`
	EventID      string         `json:"eventId,omitempty"`
	SessionID    string         `json:"sessionId"`
	StateVersion int64          `json:"stateVersion,omitempty"`
	SentAt       time.Time      `json:"sentAt"`
	RequestID    *string        `json:"requestId,omitempty"`
	Expected     *int64         `json:"expectedVersion,omitempty"`
	Payload      map[string]any `json:"payload"`
}

// Manager owns session actors and socket fan-out. PostgreSQL stays authoritative;
// actors serialize control-plane work and timers only.
type Manager struct {
	svc      *Service
	cfg      allowedOrigins
	mu       sync.Mutex
	actors   map[uuid.UUID]*actor
	clients  map[uuid.UUID]map[*client]bool
	commands map[string]any // bounded idempotent command results by requestId
	// presence tracks per-participant socket counts for grace-period leave
	// detection. Short interruptions never emit departure.
	presence map[uuid.UUID]*presenceEntry
}

type allowedOrigins struct {
	origins map[string]bool
}

func (m *Manager) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return m.cfg.origins[origin]
}

type actor struct {
	sessionID uuid.UUID
	manager   *Manager
	timer     *time.Timer
	mu        sync.Mutex
}

// NewManager builds a Manager and recovers active sessions from the database.
func NewManager(svc *Service, origins []string) *Manager {
	allowed := map[string]bool{}
	for _, o := range origins {
		if o != "" {
			allowed[o] = true
		}
	}
	m := &Manager{svc: svc, cfg: allowedOrigins{origins: allowed}, actors: map[uuid.UUID]*actor{}, clients: map[uuid.UUID]map[*client]bool{}, presence: map[uuid.UUID]*presenceEntry{}}
	svc.SetManager(m)
	go m.recover()
	return m
}

func (m *Manager) recover() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sessions, err := m.svc.Store().ListActiveSessions(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, sess := range sessions {
		m.EnsureActor(sess.ID)
		if sess.Status == StateQuestionOpen && sess.QuestionEndsAt != nil {
			if sess.QuestionEndsAt.After(now) {
				m.ScheduleClose(sess.ID, *sess.QuestionEndsAt)
			} else {
				go m.svc.TimerClose(context.Background(), sess.ID)
			}
		}
	}
}

// AllowOrigin adds a test/dev origin to the upgrade allowlist.
func (m *Manager) AllowOrigin(origin string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.origins[origin] = true
}

// RetireSession stops timers and drops actor/client state for a terminal
// session. Durable truth stays in PostgreSQL; this only bounds memory.
func (m *Manager) RetireSession(sessionID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.actors[sessionID]; ok {
		a.mu.Lock()
		if a.timer != nil {
			a.timer.Stop()
		}
		a.mu.Unlock()
		delete(m.actors, sessionID)
	}
	delete(m.clients, sessionID)
}

// ActorCount reports live actor cardinality (tests/monitoring).
func (m *Manager) ActorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.actors)
}

// EnsureActor creates the per-session actor if absent.
func (m *Manager) EnsureActor(sessionID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.actors[sessionID]; !ok {
		m.actors[sessionID] = &actor{sessionID: sessionID, manager: m}
	}
}

// ScheduleClose arms the deadline hint (idempotent DB close decides).
func (m *Manager) ScheduleClose(sessionID uuid.UUID, endsAt time.Time) {
	m.mu.Lock()
	a, ok := m.actors[sessionID]
	if !ok {
		a = &actor{sessionID: sessionID, manager: m}
		m.actors[sessionID] = a
	}
	m.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.timer != nil {
		a.timer.Stop()
	}
	delay := time.Until(endsAt)
	if delay < 0 {
		delay = 0
	}
	a.timer = time.AfterFunc(delay, func() {
		m.svc.TimerClose(context.Background(), sessionID)
	})
}

type client struct {
	conn        *websocket.Conn
	role        string // host | participant
	sessionID   uuid.UUID
	userID      *uuid.UUID
	participant *Participant
	send        chan []byte
	closed      chan struct{}
	once        sync.Once
}

// Presence grace: a participant is only reported left after this long with
// zero sockets, so short network interruptions never look like departure.
const presenceGrace = 5 * time.Second

type presenceEntry struct {
	sockets int
	timer   *time.Timer
}

// Broadcast sends a typed event to session clients with role filtering.
func (m *Manager) Broadcast(sessionID uuid.UUID, eventType string, version int64, hostPayload, publicPayload map[string]any) {
	m.mu.Lock()
	set := m.clients[sessionID]
	clients := make([]*client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	m.mu.Unlock()
	now := time.Now()
	for _, c := range clients {
		payload := publicPayload
		if c.role == "host" {
			payload = mergePayloads(publicPayload, hostPayload)
		}
		env := Envelope{V: 1, Type: eventType, EventID: eventID(sessionID, version),
			SessionID: sessionID.String(), StateVersion: version, SentAt: now, Payload: payload}
		raw, _ := json.Marshal(env)
		select {
		case c.send <- raw:
		default:
			// Slow consumer: disconnect with retryable code; never block others.
			c.closeSlow()
		}
	}
}

func mergePayloads(public, host map[string]any) map[string]any {
	if len(host) == 0 {
		return public
	}
	out := make(map[string]any, len(public)+len(host))
	for k, v := range public {
		out[k] = v
	}
	for k, v := range host {
		out[k] = v
	}
	return out
}

func eventID(sessionID uuid.UUID, n int64) string {
	return sessionID.String() + ":" + strconv.FormatInt(n, 10)
}

// trackConnect records a participant socket; a reconnect inside the grace
// window cancels the pending leave event. Duplicate identities are impossible:
// one resume token maps to exactly one participant row.
func (m *Manager) trackConnect(p Participant) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.presence[p.ID]
	if !ok {
		e = &presenceEntry{}
		m.presence[p.ID] = e
	}
	e.sockets++
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
}

// trackDisconnect arms the grace timer when a participant's last socket drops.
func (m *Manager) trackDisconnect(sessionID, participantID uuid.UUID) {
	m.mu.Lock()
	e, ok := m.presence[participantID]
	if !ok {
		e = &presenceEntry{}
		m.presence[participantID] = e
	}
	if e.sockets > 0 {
		e.sockets--
	}
	if e.sockets > 0 || e.timer != nil {
		m.mu.Unlock()
		return
	}
	e.timer = time.AfterFunc(presenceGrace, func() {
		m.emitLeft(sessionID, participantID)
	})
	m.mu.Unlock()
}

// emitLeft reports departure to the host only, after the grace window with no
// reconnect. Presence is not durable truth: answers and scores persist.
func (m *Manager) emitLeft(sessionID, participantID uuid.UUID) {
	m.mu.Lock()
	if e, ok := m.presence[participantID]; ok {
		if e.sockets > 0 {
			m.mu.Unlock()
			return // reconnected inside the window
		}
		delete(m.presence, participantID)
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := m.svc.Store().GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	m.BroadcastToRole(sessionID, "host", "participant_left", sess.StateVersion,
		map[string]any{"participantId": participantID.String(), "participantCount": sess.ParticipantCount})
}

// BroadcastPresence notifies host of join + participants of count change.
func (m *Manager) BroadcastPresence(sessionID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := m.svc.Store().GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	m.Broadcast(sessionID, "participant_count_changed", sess.StateVersion,
		map[string]any{"participantCount": sess.ParticipantCount},
		map[string]any{"participantCount": sess.ParticipantCount})
}

// BroadcastLeaderboard fans the ranked top 10 + own standing.
func (m *Manager) BroadcastLeaderboard(sessionID uuid.UUID, version int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	leaders, err := m.svc.Store().Leaderboard(ctx, sessionID, 10)
	if err != nil {
		return
	}
	public := map[string]any{"leaders": leaderDTOs(leaders)}
	m.Broadcast(sessionID, "leaderboard_updated", version, public, public)
}

// BroadcastResults fans question_results + leaderboard for the closed question.
func (m *Manager) BroadcastResults(sessionID uuid.UUID, version int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := m.svc.Store().GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	questions, err := m.svc.Store().ListQuestions(ctx, sessionID)
	if err != nil {
		return
	}
	// Results describe the most recently closed question: find the last question
	// at or before the current position (current stays set after close).
	var target *SessionQuestion
	if sess.CurrentQuestion != nil {
		target = sess.CurrentQuestion
	} else if len(questions) > 0 {
		target = &questions[len(questions)-1]
	}
	if target == nil {
		return
	}
	st, err := m.svc.Store().StatsFor(ctx, target.ID)
	if err != nil {
		return
	}
	correct := []string{}
	for _, o := range target.Options {
		if o.IsCorrect {
			correct = append(correct, o.ID.String())
		}
	}
	counts := make([]any, 0, len(target.Options))
	total := 0
	for _, o := range target.Options {
		c := st.OptionCounts[o.ID.String()]
		total += c
		counts = append(counts, map[string]any{"id": o.ID.String(), "count": c})
	}
	withPct := make([]any, 0, len(counts))
	for _, c := range counts {
		mm := c.(map[string]any)
		pct := 0.0
		if total > 0 {
			pct = float64(mm["count"].(int)) * 100 / float64(total)
		}
		mm["percentage"] = pct
		withPct = append(withPct, mm)
	}
	public := map[string]any{
		"questionId": target.ID.String(), "correctOptionIds": correct,
		"explanation": target.Explanation, "options": withPct,
	}
	host := mergePayloads(public, map[string]any{"answerCount": total})
	// Personalize ownResult per participant (correctness only after durable close).
	m.mu.Lock()
	set := m.clients[sessionID]
	clients := make([]*client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	m.mu.Unlock()
	now := time.Now()
	for _, c := range clients {
		payload := public
		if c.role == "host" {
			payload = host
		} else if c.participant != nil {
			own, ok := m.svc.Store().OwnAnswer(context.Background(), c.participant.ID, target.ID)
			mp := make(map[string]any, len(public)+1)
			for k, v := range public {
				mp[k] = v
			}
			if ok {
				mp["ownResult"] = map[string]any{"isCorrect": own.IsCorrect, "points": own.Points}
			} else {
				mp["ownResult"] = map[string]any{"isCorrect": false, "points": 0, "noAnswer": true}
			}
			payload = mp
		}
		env := Envelope{V: 1, Type: "question_results", EventID: eventID(sessionID, version),
			SessionID: sessionID.String(), StateVersion: version, SentAt: now, Payload: payload}
		raw, _ := json.Marshal(env)
		select {
		case c.send <- raw:
		default:
			c.closeSlow()
		}
	}
	m.BroadcastLeaderboard(sessionID, version)
}

// BroadcastEnded fans the terminal event then closes sockets after grace.
func (m *Manager) BroadcastEnded(sessionID uuid.UUID, version int64) {
	m.Broadcast(sessionID, "session_ended", version,
		map[string]any{"endedAt": time.Now().UTC()},
		map[string]any{"endedAt": time.Now().UTC()})
	go func() {
		time.Sleep(5 * time.Second)
		m.mu.Lock()
		set := m.clients[sessionID]
		for c := range set {
			c.closeNormal()
		}
		m.mu.Unlock()
		m.RetireSession(sessionID)
	}()
}

func leaderDTOs(leaders []LeaderRow) []any {
	out := make([]any, 0, len(leaders))
	for _, l := range leaders {
		out = append(out, map[string]any{
			"participantId": l.ParticipantID.String(), "displayName": l.DisplayName,
			"rank": l.Rank, "score": l.Score,
		})
	}
	return out
}

func (c *client) closeSlow() {
	c.once.Do(func() {
		_ = c.conn.Close(websocket.StatusCode(4008), "slow_consumer")
		close(c.closed)
	})
}

func (c *client) closeNormal() {
	c.once.Do(func() {
		_ = c.conn.Close(websocket.StatusNormalClosure, "ended")
		close(c.closed)
	})
}
