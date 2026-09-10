package mattermost_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	mattermostadapter "github.com/mclemenceau/watchtower/internal/adapters/mattermost"
	"github.com/mclemenceau/watchtower/internal/state"
)

// --- Poller tests ---

// postListResponse builds a Mattermost GET /posts response JSON string.
// posts is a slice of (id, userID, message, rootID) tuples; order is newest-first.
func postListResponse(posts []map[string]string) string {
	order := make([]string, 0, len(posts))
	postsMap := map[string]interface{}{}
	for _, p := range posts {
		id := p["id"]
		order = append(order, id)
		postsMap[id] = map[string]interface{}{
			"id":      id,
			"user_id": p["user_id"],
			"message": p["message"],
			"type":    p["type"], // "" = normal post
			"root_id": p["root_id"],
		}
	}
	b, _ := json.Marshal(map[string]interface{}{
		"order": order,
		"posts": postsMap,
	})
	return string(b)
}

// newPollerTestServer returns an httptest.Server that:
//   - GET /api/v4/channels/{channelID}/posts  calls onGetPosts and returns its response
//   - POST /api/v4/posts                       calls onPost with the decoded request body
func newPollerTestServer(
	t *testing.T,
	channelID string,
	onGetPosts func(r *http.Request) string,
	onPost func(map[string]string),
) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/channels/"+channelID+"/posts",
		func(w http.ResponseWriter, r *http.Request) {
			body := onGetPosts(r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		})

	mux.HandleFunc("/api/v4/posts",
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				var req map[string]string
				_ = json.NewDecoder(r.Body).Decode(&req)
				if onPost != nil {
					onPost(req)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"poller-reply-1"}`))
		})

	return httptest.NewServer(mux)
}

// TestRunPollerDisabledWhenNoCreds verifies RunPoller is a no-op when credentials
// are missing.
func TestRunPollerDisabledWhenNoCreds(t *testing.T) {
	var called atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	snap := state.New(t.TempDir() + "/snap.json")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// No token — must return immediately without any network call.
	mattermostadapter.RunPoller(ctx, mattermostadapter.PollerConfig{
		ServerURL: srv.URL,
		ChannelID: "ch1",
		Keyword:   "@watchtower",
	}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	if called.Load() {
		t.Error("RunPoller should not make any HTTP calls when Token is missing")
	}
}

// TestRunPollerDispatchesKeyword verifies that a post containing the keyword
// triggers a dispatch and a reply post.
func TestRunPollerDispatchesKeyword(t *testing.T) {
	const channelID = "ch1"
	var (
		mu         sync.Mutex
		postedMsgs []string
		callCount  atomic.Int32
	)

	srv := newPollerTestServer(t, channelID,
		func(r *http.Request) string {
			n := callCount.Add(1)
			if n == 1 {
				// First call: seed request (per_page=1, no after param).
				// Return an empty list so the poller starts with no watermark.
				return `{"order":[],"posts":{}}`
			}
			// Second call: return a post with the keyword.
			return postListResponse([]map[string]string{
				{
					"id": "post-1", "user_id": "user42",
					"message": "@watchtower help", "type": "", "root_id": "",
				},
			})
		},
		func(req map[string]string) {
			mu.Lock()
			postedMsgs = append(postedMsgs, req["message"])
			mu.Unlock()
		},
	)
	defer srv.Close()

	snap := state.New(t.TempDir() + "/snap.json")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	mattermostadapter.RunPoller(ctx, mattermostadapter.PollerConfig{
		ServerURL: srv.URL,
		Token:     "testtoken",
		ChannelID: channelID,
		Interval:  10 * time.Millisecond,
		Keyword:   "@watchtower",
	}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	mu.Lock()
	got := postedMsgs
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("expected at least one reply post, got none")
	}
	if !strings.Contains(got[0], "builds status") {
		t.Errorf("expected help text in reply, got: %q", got[0])
	}
}

// TestRunPollerIgnoresNonKeywordPosts verifies that posts without the keyword
// do not trigger a dispatch.
func TestRunPollerIgnoresNonKeywordPosts(t *testing.T) {
	const channelID = "ch2"
	var (
		mu         sync.Mutex
		postedMsgs []string
		callCount  atomic.Int32
	)

	srv := newPollerTestServer(t, channelID,
		func(r *http.Request) string {
			n := callCount.Add(1)
			if n == 1 {
				return `{"order":[],"posts":{}}`
			}
			return postListResponse([]map[string]string{
				{
					"id": "post-2", "user_id": "user42",
					"message": "just a normal chat message", "type": "", "root_id": "",
				},
			})
		},
		func(req map[string]string) {
			mu.Lock()
			postedMsgs = append(postedMsgs, req["message"])
			mu.Unlock()
		},
	)
	defer srv.Close()

	snap := state.New(t.TempDir() + "/snap.json")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	mattermostadapter.RunPoller(ctx, mattermostadapter.PollerConfig{
		ServerURL: srv.URL,
		Token:     "testtoken",
		ChannelID: channelID,
		Interval:  10 * time.Millisecond,
		Keyword:   "@watchtower",
	}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	mu.Lock()
	count := len(postedMsgs)
	mu.Unlock()

	if count != 0 {
		t.Errorf("expected no reply posts, got %d: %v", count, postedMsgs)
	}
}

// TestRunPollerDoesNotReplayOldPosts verifies that posts returned by the seed
// call (before any watermark is set) are not dispatched.
func TestRunPollerDoesNotReplayOldPosts(t *testing.T) {
	const channelID = "ch3"
	var (
		mu         sync.Mutex
		postedMsgs []string
		callCount  atomic.Int32
	)

	srv := newPollerTestServer(t, channelID,
		func(r *http.Request) string {
			n := callCount.Add(1)
			if n == 1 {
				// Seed call: return an existing old keyword post as the latest.
				return postListResponse([]map[string]string{
					{
						"id": "old-post", "user_id": "user42",
						"message": "@watchtower summary", "type": "", "root_id": "",
					},
				})
			}
			// Subsequent poll calls return nothing new.
			return `{"order":[],"posts":{}}`
		},
		func(req map[string]string) {
			mu.Lock()
			postedMsgs = append(postedMsgs, req["message"])
			mu.Unlock()
		},
	)
	defer srv.Close()

	snap := state.New(t.TempDir() + "/snap.json")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	mattermostadapter.RunPoller(ctx, mattermostadapter.PollerConfig{
		ServerURL: srv.URL,
		Token:     "testtoken",
		ChannelID: channelID,
		Interval:  10 * time.Millisecond,
		Keyword:   "@watchtower",
	}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	mu.Lock()
	count := len(postedMsgs)
	mu.Unlock()

	if count != 0 {
		t.Errorf("expected no replies (old post must not be replayed), got %d: %v",
			count, postedMsgs)
	}
}

// TestChannelNotifierSend verifies that ChannelNotifier POSTs to /api/v4/posts
// with the correct Authorization header and message body.
func TestChannelNotifierSend(t *testing.T) {
	var (
		mu           sync.Mutex
		receivedAuth string
		receivedMsg  string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/posts" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		receivedAuth = r.Header.Get("Authorization")
		receivedMsg = req["message"]
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"post1"}`))
	}))
	defer srv.Close()

	n := mattermostadapter.NewChannelNotifier(srv.URL, "mytoken", "ch123")
	if err := n.Send("hello world"); err != nil {
		t.Fatalf("Send returned unexpected error: %v", err)
	}

	mu.Lock()
	gotAuth, gotMsg := receivedAuth, receivedMsg
	mu.Unlock()

	if gotAuth != "Bearer mytoken" {
		t.Errorf("expected Authorization header %q, got %q", "Bearer mytoken", gotAuth)
	}
	if gotMsg != "hello world" {
		t.Errorf("expected message %q, got %q", "hello world", gotMsg)
	}
}

// TestRunBotDisabledWhenNoCreds verifies that RunBot is a no-op when credentials are missing.
func TestRunBotDisabledWhenNoCreds(t *testing.T) {
	var httpCalled atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalled.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	snap := state.New(t.TempDir() + "/snap.json")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// No token — RunBot must return immediately without making any network call.
	mattermostadapter.RunBot(ctx, mattermostadapter.BotConfig{
		ServerURL: srv.URL,
		BotUserID: "bot1",
		Keyword:   "@watchtower",
	}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	if httpCalled.Load() {
		t.Error("RunBot should not make any HTTP/WS calls when Token is missing")
	}
}

// wsUpgrader is a shared WebSocket upgrader used in tests.
var wsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// postedEvent builds a Mattermost "posted" WebSocket event for a user post.
// id is the post ID, rootID is non-empty when the post is a thread reply.
func postedEvent(channelID, userID, id, rootID, message string) map[string]interface{} {
	postJSON, _ := json.Marshal(map[string]interface{}{
		"id":         id,
		"channel_id": channelID,
		"user_id":    userID,
		"message":    message,
		"type":       "",
		"root_id":    rootID,
	})
	return map[string]interface{}{
		"event": "posted",
		"data": map[string]interface{}{
			"post": string(postJSON),
		},
		"broadcast": map[string]interface{}{
			"channel_id": channelID,
			"user_id":    userID,
		},
	}
}

// newTestServer creates a combined HTTP+WebSocket test server.
// wsEvents is a function that is called once the WS connection is established
// (after consuming the auth challenge) to deliver events to the bot.
// Each POST to /api/v4/posts calls onPost with the request body map.
func newTestServer(t *testing.T, wsEvents func(conn *websocket.Conn), onPost func(map[string]string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/websocket", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()           //nolint:errcheck
		_, _, _ = conn.ReadMessage() // consume auth challenge
		wsEvents(conn)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	mux.HandleFunc("/api/v4/posts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if onPost != nil {
				onPost(req)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"bot-reply-1"}`))
	})

	return httptest.NewServer(mux)
}

// TestRunBotDispatchesKeywordMention verifies that RunBot dispatches a command
// and posts a response when a @watchtower mention arrives over WebSocket.
func TestRunBotDispatchesKeywordMention(t *testing.T) {
	var (
		mu        sync.Mutex
		postedMsg string
	)

	srv := newTestServer(t,
		func(conn *websocket.Conn) {
			_ = conn.WriteJSON(postedEvent("ch1", "user42", "post-user-1", "", "@watchtower help"))
		},
		func(req map[string]string) {
			mu.Lock()
			postedMsg = req["message"]
			mu.Unlock()
		},
	)
	defer srv.Close()

	snap := state.New(t.TempDir() + "/snap.json")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	mattermostadapter.RunBot(ctx, mattermostadapter.BotConfig{
		ServerURL:      srv.URL,
		Token:          "testtoken",
		BotUserID:      "botuser",
		Keyword:        "@watchtower",
		ReconnectDelay: 10 * time.Millisecond,
	}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	mu.Lock()
	got := postedMsg
	mu.Unlock()

	if !strings.Contains(got, "builds status") {
		t.Errorf("expected help text in posted reply, got: %q", got)
	}
}

// TestRunBotMidSentenceAndBareMentions covers two patterns:
//   - keyword mid-sentence with a command after it: "hello @watchtower help"
//   - keyword with nothing after it: "hello @watchtower" → greeting
func TestRunBotMidSentenceAndBareMentions(t *testing.T) {
	cases := []struct {
		message  string
		wantText string // substring expected in the bot's reply
	}{
		// Command follows keyword mid-sentence.
		{"hello @watchtower help", "builds status"},
		// Nothing after keyword — should get the greeting, not the help table.
		{"hello @watchtower", "Watchtower"},
		// Bare leading keyword — same greeting.
		{"@watchtower", "Watchtower"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.message, func(t *testing.T) {
			var (
				mu        sync.Mutex
				postedMsg string
			)

			srv := newTestServer(t,
				func(conn *websocket.Conn) {
					_ = conn.WriteJSON(postedEvent("ch1", "user42", "post-user-1", "", tc.message))
				},
				func(req map[string]string) {
					mu.Lock()
					postedMsg = req["message"]
					mu.Unlock()
				},
			)
			defer srv.Close()

			snap := state.New(t.TempDir() + "/snap.json")
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			mattermostadapter.RunBot(ctx, mattermostadapter.BotConfig{
				ServerURL:      srv.URL,
				Token:          "testtoken",
				BotUserID:      "botuser",
				Keyword:        "@watchtower",
				ReconnectDelay: 10 * time.Millisecond,
			}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

			mu.Lock()
			got := postedMsg
			mu.Unlock()

			if !strings.Contains(got, tc.wantText) {
				t.Errorf("message %q: expected reply to contain %q, got: %q", tc.message, tc.wantText, got)
			}
		})
	}
}

// TestRunBotIgnoresThreadReplyWithoutKeyword verifies that the bot does NOT
// respond to a thread reply that does not contain the keyword. Two users (or
// one user continuing a conversation) should be able to talk in a thread the
// bot has replied to without triggering the bot every time.
func TestRunBotIgnoresThreadReplyWithoutKeyword(t *testing.T) {
	var (
		mu         sync.Mutex
		postedMsgs []string
	)

	srv := newTestServer(t,
		func(conn *websocket.Conn) {
			// Step 1: user mentions @watchtower — bot should reply (1 post expected).
			_ = conn.WriteJSON(postedEvent("ch1", "user42", "post-user-1", "", "@watchtower summary"))

			time.Sleep(50 * time.Millisecond)

			// Step 2: user (or another user) replies in the thread with no keyword.
			// Bot must stay silent.
			_ = conn.WriteJSON(postedEvent("ch1", "user42", "post-user-2", "post-user-1", "sounds good, thanks"))

			time.Sleep(50 * time.Millisecond)

			// Step 3: another bare thread reply — bot must stay silent.
			_ = conn.WriteJSON(postedEvent("ch1", "user99", "post-user-3", "post-user-1", "agreed"))

			time.Sleep(50 * time.Millisecond)
		},
		func(req map[string]string) {
			mu.Lock()
			postedMsgs = append(postedMsgs, req["message"])
			mu.Unlock()
		},
	)
	defer srv.Close()

	snap := state.New(t.TempDir() + "/snap.json")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	mattermostadapter.RunBot(ctx, mattermostadapter.BotConfig{
		ServerURL:      srv.URL,
		Token:          "testtoken",
		BotUserID:      "botuser",
		Keyword:        "@watchtower",
		ReconnectDelay: 10 * time.Millisecond,
	}, snap, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	mu.Lock()
	count := len(postedMsgs)
	mu.Unlock()

	// Only the initial @watchtower mention should have produced a reply.
	// Thread replies without the keyword must be silently ignored.
	if count != 1 {
		t.Fatalf("expected exactly 1 bot post (initial mention only), got %d: %v", count, postedMsgs)
	}
}
