package githubserver

import (
	cryptorand "crypto/rand"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/store/teststore"
	"github.com/icholy/xagent/internal/x/githubx"
	"gotest.tools/v3/assert"
)

// reactStub stands up a fake GitHub REST API: it answers the installation
// token-mint endpoint and records the path of every reaction request so tests
// can assert which endpoint react chose (or that it made none).
type reactStub struct {
	server    *httptest.Server
	mu        sync.Mutex
	reactions []string
}

func newReactStub(t *testing.T) *reactStub {
	t.Helper()
	stub := &reactStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"ghs_test","expires_at":"2099-01-01T00:00:00Z"}`)
		case strings.HasSuffix(r.URL.Path, "/reactions"):
			stub.mu.Lock()
			stub.reactions = append(stub.reactions, r.URL.Path)
			stub.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"eyes"}`)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *reactStub) reactionPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reactions...)
}

// newReactServer builds a Server whose token cache and clients are pointed at
// the stub, so react talks to it instead of the real GitHub API.
func newReactServer(t *testing.T, stub *reactStub) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	assert.NilError(t, err)
	app := ghinstallation.NewAppsTransportFromPrivateKey(http.DefaultTransport, 12345, key)
	app.BaseURL = stub.server.URL
	tokens, err := githubx.NewAppTokenCache(app, githubx.AppTokenCacheOptions{BaseURL: stub.server.URL})
	assert.NilError(t, err)
	return &Server{
		log:    slog.Default(),
		store:  teststore.New(t),
		tokens: tokens,
	}
}

func TestReact(t *testing.T) {
	const installationID = 999

	tests := []struct {
		name         string
		meta         any
		eventType    string
		installation int64  // 0 -> leave the org without an installation
		wantPath     string // "" -> expect no reaction request
	}{
		{
			name:         "issue comment",
			meta:         GitHubMeta{Owner: "octo", Repo: "repo", CommentID: 42},
			eventType:    EventTypeIssueComment,
			installation: installationID,
			wantPath:     "/repos/octo/repo/issues/comments/42/reactions",
		},
		{
			name:         "pull request review comment",
			meta:         GitHubMeta{Owner: "octo", Repo: "repo", CommentID: 7},
			eventType:    EventTypePullRequestReviewComment,
			installation: installationID,
			wantPath:     "/repos/octo/repo/pulls/comments/7/reactions",
		},
		{
			name:         "non-github meta is a no-op",
			meta:         "not-a-github-meta",
			eventType:    EventTypeIssueComment,
			installation: installationID,
		},
		{
			name:         "zero comment id is a no-op",
			meta:         GitHubMeta{Owner: "octo", Repo: "repo", CommentID: 0},
			eventType:    EventTypeIssueComment,
			installation: installationID,
		},
		{
			name:         "non-reactable type is a no-op",
			meta:         GitHubMeta{Owner: "octo", Repo: "repo", CommentID: 42},
			eventType:    EventTypePullRequestReview,
			installation: installationID,
		},
		{
			name:         "no installation is a no-op",
			meta:         GitHubMeta{Owner: "octo", Repo: "repo", CommentID: 42},
			eventType:    EventTypeIssueComment,
			installation: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newReactStub(t)
			s := newReactServer(t, stub)

			org := teststore.CreateOrg(t, s.store, nil)
			if tt.installation != 0 {
				err := s.store.SetOrgGitHubInstallation(t.Context(), nil, org.OrgID, tt.installation)
				assert.NilError(t, err)
			}

			err := s.react(t.Context(), eventrouter.RouteOutcome{
				OrgID: org.OrgID,
				Input: eventrouter.InputEvent{Type: tt.eventType, Meta: tt.meta},
			})
			assert.NilError(t, err)

			paths := stub.reactionPaths()
			if tt.wantPath == "" {
				assert.Equal(t, len(paths), 0, "expected no reaction request, got %v", paths)
				return
			}
			assert.DeepEqual(t, paths, []string{tt.wantPath})
		})
	}
}
