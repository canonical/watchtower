package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mclemenceau/watchtower/internal/application"
	"github.com/mclemenceau/watchtower/internal/domain"
	"github.com/mclemenceau/watchtower/internal/intent"
	"github.com/mclemenceau/watchtower/internal/ports"
	"github.com/mclemenceau/watchtower/internal/state"
)

// PollerConfig holds all parameters for the REST-polling Mattermost integration.
// This mode requires only a personal access token (no bot account needed).
type PollerConfig struct {
	// ServerURL is the base HTTP URL of the Mattermost server.
	ServerURL string
	// Token is a personal access token for any Mattermost user account.
	Token string
	// ChannelID is the single channel to watch for keyword mentions.
	ChannelID string
	// Interval is how often to poll for new posts. Defaults to 30s.
	Interval time.Duration
	// Keyword is the trigger prefix (e.g. "@watchtower"). Case-insensitive.
	Keyword string
}

// mmPostList is the subset of the Mattermost GET /posts response we need.
type mmPostList struct {
	Order []string           `json:"order"` // post IDs newest-first
	Posts map[string]mmRPost `json:"posts"`
}

// mmRPost is a post entry inside the post list response.
type mmRPost struct {
	ID      string `json:"id"`
	UserID  string `json:"user_id"`
	Message string `json:"message"`
	Type    string `json:"type"`    // "" = normal user post; "system_*" = system
	RootID  string `json:"root_id"` // non-empty when the post is a thread reply
}

// RunPoller polls the Mattermost REST API for new posts in a single channel,
// dispatches any post that contains the keyword, and replies in the thread.
// It is a no-op when any of ServerURL, Token, or ChannelID is empty.
//
// The notifier passed in is used for proactive (scheduled) summary posts to
// the same channel; it should be a ChannelNotifier wrapping the same channel.
func RunPoller(
	ctx context.Context,
	cfg PollerConfig,
	snap *state.Snapshot,
	failures ports.FailureStorePort,
	releasesScope []string,
	summaryForProducts []string,
	httpClient *http.Client,
	resolver *intent.Resolver,
	logFetcher ports.LogFetcher,
	llm ports.LLMClient,
	launchpad ports.LaunchpadSource,
	triggerAnalysis func(release string) error,
) {
	if cfg.Token == "" || cfg.ServerURL == "" || cfg.ChannelID == "" {
		log.Print("mattermost poller: disabled" +
			" (set MATTERMOST_SERVER_URL, MATTERMOST_POLL_TOKEN," +
			" MATTERMOST_CHANNEL_ID to enable)")
		return
	}

	keyword := strings.ToLower(strings.TrimSpace(cfg.Keyword))
	if keyword == "" {
		keyword = "@watchtower"
	}

	interval := cfg.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	baseURL := strings.TrimRight(cfg.ServerURL, "/")

	log.Printf("mattermost poller: starting on channel %s (interval: %s, keyword: %q)",
		cfg.ChannelID, interval, keyword)

	// Seed lastPostID with the most recent post in the channel so we don't
	// replay history on startup.
	lastPostID := fetchLatestPostID(ctx, baseURL, cfg.Token, cfg.ChannelID, httpClient)
	log.Printf("mattermost poller: seeded at post %q", lastPostID)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Print("mattermost poller: context cancelled, shutting down")
			return
		case <-ticker.C:
			lastPostID = pollOnce(
				ctx, baseURL, cfg.Token, cfg.ChannelID,
				keyword, lastPostID,
				snap, failures, releasesScope, summaryForProducts,
				httpClient, resolver, logFetcher, llm, launchpad,
				triggerAnalysis,
			)
		}
	}
}

// fetchLatestPostID returns the ID of the most recent post in the channel,
// or "" when the channel is empty or the request fails (safe default: replay
// nothing from before this run, but still process everything after).
func fetchLatestPostID(
	ctx context.Context,
	baseURL, token, channelID string,
	httpClient *http.Client,
) string {
	u := fmt.Sprintf("%s/api/v4/channels/%s/posts?per_page=1", baseURL, channelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var pl mmPostList
	if err := json.NewDecoder(resp.Body).Decode(&pl); err != nil {
		return ""
	}
	if len(pl.Order) == 0 {
		return ""
	}
	return pl.Order[0] // newest post ID
}

// pollOnce fetches posts newer than afterPostID, dispatches keyword matches,
// and returns the new high-water mark (most recent post ID seen, whether it
// matched or not, so we never re-process the same posts).
func pollOnce(
	ctx context.Context,
	baseURL, token, channelID string,
	keyword, afterPostID string,
	snap *state.Snapshot,
	failures ports.FailureStorePort,
	releasesScope []string,
	summaryForProducts []string,
	httpClient *http.Client,
	resolver *intent.Resolver,
	logFetcher ports.LogFetcher,
	llm ports.LLMClient,
	launchpad ports.LaunchpadSource,
	triggerAnalysis func(release string) error,
) string {
	u := fmt.Sprintf("%s/api/v4/channels/%s/posts?per_page=50", baseURL, channelID)
	if afterPostID != "" {
		u += "&after=" + afterPostID
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		log.Printf("mattermost poller: build request: %v", err)
		return afterPostID
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("mattermost poller: fetch posts: %v", err)
		return afterPostID
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		log.Printf("mattermost poller: mattermost returned %d: %s",
			resp.StatusCode, body)
		return afterPostID
	}

	var pl mmPostList
	if err := json.NewDecoder(resp.Body).Decode(&pl); err != nil {
		log.Printf("mattermost poller: decode posts: %v", err)
		return afterPostID
	}

	if len(pl.Order) == 0 {
		return afterPostID
	}

	// pl.Order is newest-first. Process oldest-first so dispatch order is
	// chronological, then advance the watermark to the newest.
	for i := len(pl.Order) - 1; i >= 0; i-- {
		id := pl.Order[i]
		post, ok := pl.Posts[id]
		if !ok {
			continue
		}

		// Skip system messages.
		if post.Type != "" {
			continue
		}

		lower := strings.ToLower(strings.TrimSpace(post.Message))
		if !strings.Contains(lower, keyword) {
			continue
		}

		// Extract command: everything after the keyword.
		idx := strings.Index(lower, keyword)
		cmd := strings.TrimSpace(post.Message[idx+len(keyword):])
		if cmd == "" {
			cmd = "greet"
		}

		artefacts, err := snap.Read()
		if err != nil {
			log.Printf("mattermost poller: read snapshot: %v", err)
			continue
		}

		var failureStore domain.FailureStore
		if failures != nil {
			if fs, ferr := failures.ReadFailures(); ferr == nil {
				failureStore = fs
			}
		}
		if failureStore == nil {
			failureStore = make(domain.FailureStore)
		}

		// Reply in thread: if the post is already in a thread use that root,
		// otherwise root the thread at the triggering post itself.
		channelN := NewChannelNotifier(baseURL, token, channelID)
		threadRoot := post.RootID
		if threadRoot == "" {
			threadRoot = post.ID
		}
		notifier := &ThreadNotifier{channel: channelN, rootID: threadRoot}

		sessionID := channelID + ":" + post.UserID

		go func(n ports.Notifier, sid, command string, fs domain.FailureStore) {
			if err := application.Dispatch(
				ctx, sid, command, artefacts, fs,
				releasesScope, summaryForProducts,
				n, "", resolver, logFetcher, llm, launchpad,
				triggerAnalysis,
			); err != nil {
				log.Printf("mattermost poller: dispatch %q: %v", command, err)
			}
		}(notifier, sessionID, cmd, failureStore)
	}

	// Advance watermark to the newest post seen this poll.
	return pl.Order[0]
}
