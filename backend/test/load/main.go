// Command loadharness runs reproducible live load scenarios against a running API.
//
// Usage:
//
//	go run ./test/load -base http://127.0.0.1:18085 -mode burst -n 200
//	go run ./test/load -base ... -mode multi -sessions 10 -per 20
//	go run ./test/load -base ... -mode soak -minutes 15
//
// Modes share one flow: register host → create/publish quiz → start session(s) →
// join N participants → open question → burst answers → end. Soak cycles questions
// with reconnect churn for the requested duration and samples server RSS.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

var (
	base      = flag.String("base", "http://127.0.0.1:18085", "API base URL (http)")
	wsBase    = flag.String("ws", "", "WS base URL (default: derived from -base)")
	mode      = flag.String("mode", "burst", "burst|multi|soak")
	n         = flag.Int("n", 200, "participants for burst mode")
	sessions  = flag.Int("sessions", 10, "sessions for multi mode")
	per       = flag.Int("per", 20, "participants per session for multi/soak")
	minutes   = flag.Int("minutes", 15, "soak duration")
	pace      = flag.Int("pace", 0, "seconds to wait between soak rounds (0 = back-to-back)")
	serverpid = flag.Int("serverpid", 0, "API server PID for RSS sampling (0 = skip)")
)

func main() {
	flag.Parse()
	if *wsBase == "" {
		*wsBase = "ws" + (*base)[4:]
	}
	switch *mode {
	case "burst":
		runBurst(*n)
	case "multi":
		runMulti(*sessions, *per)
	case "soak":
		runSoak(*minutes)
	default:
		fmt.Println("unknown mode")
		os.Exit(1)
	}
}

type client struct {
	hc   *http.Client
	csrf string
}

func newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{hc: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

func (c *client) do(method, path string, body any, headers map[string]string) (int, []byte) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, *base+path, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func registerHost(tag string) *client {
	c := newClient()
	st, b := c.do("POST", "/api/v1/auth/register", map[string]string{
		"email": tag + "@load.local", "username": "h_" + tag, "displayName": "H", "password": "load-long-passphrase-1234",
	}, nil)
	if st != 201 {
		panic(fmt.Sprintf("register %d %s", st, b))
	}
	var out struct {
		Data struct {
			CSRFToken string `json:"csrfToken"`
		} `json:"data"`
	}
	_ = json.Unmarshal(b, &out)
	c.csrf = out.Data.CSRFToken
	return c
}

func (c *client) authed(extra map[string]string) map[string]string {
	h := map[string]string{"X-CSRF-Token": c.csrf, "Origin": "http://localhost:5173"}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func makeQuiz(c *client, title string, questions int, timeLimit int) string {
	st, b := c.do("POST", "/api/v1/quizzes", map[string]string{"title": title}, c.authed(nil))
	if st != 201 {
		panic("create quiz: " + string(b))
	}
	var q struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(b, &q)
	qs := []any{}
	for i := 0; i < questions; i++ {
		qs = append(qs, map[string]any{
			"type": "multiple_choice", "position": i, "prompt": fmt.Sprintf("Question %d: pick B?", i+1),
			"explanation": "B.", "difficulty": "easy", "timeLimitSeconds": timeLimit,
			"scoringMode": "speed", "basePoints": 1000,
			"options": []any{
				map[string]any{"position": 0, "text": "A", "isCorrect": false},
				map[string]any{"position": 1, "text": "B", "isCorrect": true},
			},
		})
	}
	st, _ = c.do("PATCH", "/api/v1/quizzes/"+q.Data.ID, map[string]any{"title": title, "description": "", "questions": qs}, c.authed(map[string]string{"If-Match": `"1"`}))
	if st != 200 {
		panic("update quiz")
	}
	st, _ = c.do("POST", "/api/v1/quizzes/"+q.Data.ID+"/publish", nil, c.authed(map[string]string{"If-Match": `"2"`}))
	if st != 200 {
		panic("publish quiz")
	}
	return q.Data.ID
}

func startSession(c *client, quizID string) (id, code string) {
	st, b := c.do("POST", "/api/v1/quizzes/"+quizID+"/live-sessions", map[string]any{"allowLateJoin": true}, c.authed(map[string]string{"Idempotency-Key": uuid.NewString()}))
	if st != 201 {
		panic("start: " + string(b))
	}
	var out struct {
		Data struct {
			ID       string `json:"id"`
			JoinCode string `json:"joinCode"`
		} `json:"data"`
	}
	_ = json.Unmarshal(b, &out)
	return out.Data.ID, out.Data.JoinCode
}

func joinParticipant(code, name string) *cookiejar.Jar {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	b, _ := json.Marshal(map[string]string{"displayName": name})
	req, _ := http.NewRequest("POST", *base+"/api/v1/join/"+code, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")
	resp, err := hc.Do(req)
	if err != nil {
		panic(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		panic(fmt.Sprintf("join %d", resp.StatusCode))
	}
	return jar
}

func dialWS(jar http.CookieJar, sessionID string) *websocket.Conn {
	u, _ := url.Parse(*base)
	var hdr string
	for _, ck := range jar.Cookies(u) {
		hdr += ck.Name + "=" + ck.Value + "; "
	}
	conn, _, err := websocket.Dial(context.Background(), *wsBase+"/ws/v1/sessions/"+sessionID,
		&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {hdr}, "Origin": {"http://localhost:5173"}}})
	if err != nil {
		panic(err)
	}
	return conn
}

func readUntil(c *websocket.Conn, types ...string) map[string]any {
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	for i := 0; i < 128; i++ {
		env := readOne(c, 15*time.Second)
		if t, _ := env["type"].(string); want[t] {
			return env
		}
	}
	panic("never saw " + strings.Join(types, "/"))
}

func readOne(c *websocket.Conn, timeout time.Duration) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, raw, err := c.Read(ctx)
	if err != nil {
		panic(err)
	}
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	return env
}

func writeCmd(c *websocket.Conn, v any) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, _ := json.Marshal(v)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		panic(err)
	}
}

type hist struct {
	mu   sync.Mutex
	vals []float64
}

func (h *hist) add(v float64) {
	h.mu.Lock()
	h.vals = append(h.vals, v)
	h.mu.Unlock()
}

func (h *hist) pct(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.vals) == 0 {
		return 0
	}
	s := append([]float64(nil), h.vals...)
	sort.Float64s(s)
	i := int(math.Ceil(p/100*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	return s[i]
}

func runBurst(n int) {
	host := registerHost(fmt.Sprintf("burst%d", time.Now().UnixNano()%100000))
	quizID := makeQuiz(host, "Burst Quiz", 2, 120)
	sid, code := startSession(host, quizID)
	fmt.Printf("session=%s code=%s\n", sid, code)

	t0 := time.Now()
	jars := make([]*cookiejar.Jar, n)
	var wg sync.WaitGroup
	for i := range jars {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			jars[i] = joinParticipant(code, fmt.Sprintf("Burst-%04d", i))
		}(i)
	}
	wg.Wait()
	joinDur := time.Since(t0)

	hostConn := dialWS(host.hc.Jar, sid)
	defer hostConn.Close(websocket.StatusNormalClosure, "done")
	readOne(hostConn, 10*time.Second)
	writeCmd(hostConn, map[string]any{"v": 1, "type": "host.start_question", "sessionId": sid, "sentAt": time.Now().UTC(), "requestId": uuid.NewString(), "expectedVersion": 1, "payload": map[string]any{"questionPosition": 0}})
	readOne(hostConn, 10*time.Second)
	readOne(hostConn, 10*time.Second)

	// Public snapshot for option IDs.
	optIDs := publicOptions(jars[0], sid)

	acks := hist{}
	var ok, fail int64
	var mu sync.Mutex
	t1 := time.Now()
	for i := range jars {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn := dialWS(jars[i], sid)
			defer conn.Close(websocket.StatusNormalClosure, "done")
			readOne(conn, 15*time.Second)
			sub := uuid.NewString()
			start := time.Now()
			writeCmd(conn, map[string]any{"v": 1, "type": "participant.submit_answer", "sessionId": sid, "sentAt": time.Now().UTC(), "requestId": uuid.NewString(), "payload": map[string]any{"submissionId": sub, "questionId": optIDs.qid, "optionIds": []string{optIDs.oid}}})
			env := readOne(conn, 15*time.Second)
			el := float64(time.Since(start).Milliseconds())
			mu.Lock()
			defer mu.Unlock()
			if env["type"] == "answer_acknowledged" {
				acks.add(el)
				ok++
			} else {
				fail++
			}
		}(i)
	}
	wg.Wait()
	ansDur := time.Since(t1)
	fmt.Printf("BURST n=%d joins=%v answers=%v accepted=%d failed=%d ack_ms_p50=%.0f p95=%.0f p99=%.0f\n",
		n, joinDur, ansDur, ok, fail, acks.pct(50), acks.pct(95), acks.pct(99))
}

type optPair struct {
	qid, oid string
}

func publicOptions(jar *cookiejar.Jar, sid string) optPair {
	for attempt := 0; attempt < 10; attempt++ {
		if p, ok := tryPublicOptions(jar, sid); ok {
			return p
		}
		time.Sleep(200 * time.Millisecond)
	}
	panic("no open question in snapshot")
}

func tryPublicOptions(jar *cookiejar.Jar, sid string) (optPair, bool) {
	u, _ := url.Parse(*base)
	req, _ := http.NewRequest("GET", *base+"/api/v1/play/"+sid+"/snapshot", nil)
	for _, ck := range jar.Cookies(u) {
		req.AddCookie(ck)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var snap struct {
		Data struct {
			Question struct {
				ID      string `json:"id"`
				Options []struct {
					ID string `json:"id"`
				} `json:"options"`
			} `json:"question"`
		} `json:"data"`
	}
	_ = json.Unmarshal(b, &snap)
	if snap.Data.Question.ID == "" || len(snap.Data.Question.Options) == 0 {
		return optPair{}, false
	}
	return optPair{snap.Data.Question.ID, snap.Data.Question.Options[0].ID}, true
}

func runMulti(sessions, per int) {
	host := registerHost(fmt.Sprintf("multi%d", time.Now().UnixNano()%100000))
	quizID := makeQuiz(host, "Multi Quiz", 2, 120)
	type S struct {
		id, code string
	}
	ss := make([]S, sessions)
	for i := range ss {
		id, code := startSession(host, quizID)
		ss[i] = S{id, code}
	}
	var ok, fail int64
	var mu sync.Mutex
	t0 := time.Now()
	var wg sync.WaitGroup
	for _, s := range ss {
		s := s
		hostConn := dialWS(host.hc.Jar, s.id)
		readOne(hostConn, 10*time.Second)
		writeCmd(hostConn, map[string]any{"v": 1, "type": "host.start_question", "sessionId": s.id, "sentAt": time.Now().UTC(), "requestId": uuid.NewString(), "expectedVersion": 1, "payload": map[string]any{"questionPosition": 0}})
		readUntil(hostConn, "command_ack", "question_started")
		readUntil(hostConn, "command_ack", "question_started")
		jars := make([]*cookiejar.Jar, per)
		for i := range jars {
			jars[i] = joinParticipant(s.code, fmt.Sprintf("M%s-%02d", s.code[:2], i))
		}
		opts := publicOptions(jars[0], s.id)
		for i := range jars {
			wg.Add(1)
			go func(s S, i int) {
				defer wg.Done()
				conn := dialWS(jars[i], s.id)
				defer conn.Close(websocket.StatusNormalClosure, "done")
				readOne(conn, 15*time.Second)
				writeCmd(conn, map[string]any{"v": 1, "type": "participant.submit_answer", "sessionId": s.id, "sentAt": time.Now().UTC(), "requestId": uuid.NewString(), "payload": map[string]any{"submissionId": uuid.NewString(), "questionId": opts.qid, "optionIds": []string{opts.oid}}})
				env := readOne(conn, 15*time.Second)
				mu.Lock()
				defer mu.Unlock()
				if env["type"] == "answer_acknowledged" {
					ok++
				} else {
					fail++
				}
			}(s, i)
		}
		hostConn.Close(websocket.StatusNormalClosure, "done")
	}
	wg.Wait()
	fmt.Printf("MULTI sessions=%d per=%d answers=%v accepted=%d failed=%d\n", sessions, per, time.Since(t0), ok, fail)
}

func runSoak(minutes int) {
	host := registerHost(fmt.Sprintf("soak%d", time.Now().UnixNano()%100000))
	quizID := makeQuiz(host, "Soak Quiz", 20, 60)
	sid, code := startSession(host, quizID)
	const per = 10
	jars := make([]*cookiejar.Jar, per)
	for i := range jars {
		jars[i] = joinParticipant(code, fmt.Sprintf("Soak-%02d", i))
	}
	hostJar := host.hc.Jar
	deadline := time.Now().Add(time.Duration(minutes) * time.Minute)
	pos := 0
	var answers, errors int
	t0 := time.Now()
	// Persistent watcher: one long-lived connection with a single read context
	// (no expiry during the test) proving idle survival + continuous delivery.
	watcher := dialWS(hostJar, sid)
	defer watcher.Close(websocket.StatusNormalClosure, "done")
	watchCtx, watchCancel := context.WithTimeout(context.Background(), time.Duration(minutes+2)*time.Minute)
	defer watchCancel()
	watchSeen := make(chan string, 1024)
	go func() {
		for {
			_, raw, err := watcher.Read(watchCtx)
			if err != nil {
				return
			}
			var env map[string]any
			_ = json.Unmarshal(raw, &env)
			if t, _ := env["type"].(string); t != "" {
				select {
				case watchSeen <- t:
				default:
				}
			}
		}
	}()
	for time.Now().Before(deadline) && pos < 20 {
		// Fresh host control connection per round (also exercises reconnect).
		hostConn := dialWS(hostJar, sid)
		snap := readOne(hostConn, 10*time.Second)
		version := int64(snap["stateVersion"].(float64))
		writeCmd(hostConn, map[string]any{"v": 1, "type": "host.start_question", "sessionId": sid, "sentAt": time.Now().UTC(), "requestId": uuid.NewString(), "expectedVersion": version, "payload": map[string]any{"questionPosition": pos}})
		version++
		readUntil(hostConn, "question_started")
		opts := publicOptions(jars[0], sid)
		var wg sync.WaitGroup
		for i := range jars {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				// Reconnect churn: fresh socket per answer round.
				conn := dialWS(jars[i], sid)
				defer conn.Close(websocket.StatusNormalClosure, "done")
				readOne(conn, 15*time.Second)
				writeCmd(conn, map[string]any{"v": 1, "type": "participant.submit_answer", "sessionId": sid, "sentAt": time.Now().UTC(), "requestId": uuid.NewString(), "payload": map[string]any{"submissionId": uuid.NewString(), "questionId": opts.qid, "optionIds": []string{opts.oid}}})
				if env := readOne(conn, 15*time.Second); env["type"] == "answer_acknowledged" {
					answers++
				} else {
					errors++
				}
			}(i)
		}
		wg.Wait()
		writeCmd(hostConn, map[string]any{"v": 1, "type": "host.end_question", "sessionId": sid, "sentAt": time.Now().UTC(), "requestId": uuid.NewString(), "expectedVersion": version, "payload": map[string]any{}})
		version++
		readUntil(hostConn, "question_results")
		hostConn.Close(websocket.StatusNormalClosure, "done")
		pos++
		fmt.Printf("soak q=%d elapsed=%v answers=%d errors=%d %s\n", pos, time.Since(t0).Round(time.Second), answers, errors, rss())
		if *pace > 0 && time.Now().Before(deadline) && pos < 20 {
			time.Sleep(time.Duration(*pace) * time.Second)
		}
	}
	watchTypes := map[string]int{}
drain:
	for {
		select {
		case t := <-watchSeen:
			watchTypes[t]++
		default:
			break drain
		}
	}
	fmt.Printf("SOAK minutes=%d questions=%d answers=%d errors=%d elapsed=%v watcher=%v %s\n", minutes, pos, answers, errors, time.Since(t0), watchTypes, rss())
}

func rss() string {
	if *serverpid == 0 {
		return ""
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", *serverpid))
	if err != nil {
		return "rss=?"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			return "server_" + strings.Join(strings.Fields(line)[1:], "")
		}
	}
	return "rss=?"
}
