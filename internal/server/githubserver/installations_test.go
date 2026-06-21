package githubserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/go-github/v88/github"
	"gotest.tools/v3/assert"

	"github.com/icholy/xagent/internal/model"
)

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	assert.NilError(t, json.NewEncoder(w).Encode(v))
}

// ghFixture configures the fake GitHub API used to exercise
// verifyInstallationAccess. Zero-valued fields make the corresponding endpoint
// return 404.
type ghFixture struct {
	installationID int64
	account        *github.User // returned by GET /app/installations/{id}

	// userByID maps a GitHub user id to the login GET /user/{id} resolves to.
	userByID map[int64]string
	// membership maps "org/login" to the membership returned by
	// GET /orgs/{org}/memberships/{login}; absent entries 404.
	membership map[string]*github.Membership

	// requestedMembershipLogin records the login the membership endpoint was
	// queried with, so a test can assert the resolved (not the stale) login was used.
	requestedMembershipLogin string
}

// newGitHubFixture spins up an httptest server speaking enough of the GitHub API
// for verifyInstallationAccess and returns app + installation clients pointed at it.
func newGitHubFixture(t *testing.T, fx *ghFixture) (*github.Client, *github.Client) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/", func(w http.ResponseWriter, r *http.Request) {
		if fx.account == nil {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		writeJSON(t, w, &github.Installation{
			ID:      github.Ptr(fx.installationID),
			Account: fx.account,
		})
	})
	mux.HandleFunc("/user/", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/user/"), 10, 64)
		if err != nil {
			http.Error(w, `{"message":"bad id"}`, http.StatusBadRequest)
			return
		}
		login, ok := fx.userByID[id]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		writeJSON(t, w, &github.User{ID: github.Ptr(id), Login: github.Ptr(login)})
	})
	mux.HandleFunc("/orgs/", func(w http.ResponseWriter, r *http.Request) {
		// path: /orgs/{org}/memberships/{login}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 4 || parts[2] != "memberships" {
			http.Error(w, `{"message":"bad path"}`, http.StatusBadRequest)
			return
		}
		org, login := parts[1], parts[3]
		fx.requestedMembershipLogin = login
		m, ok := fx.membership[org+"/"+login]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		writeJSON(t, w, m)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	base := srv.URL + "/"
	appClient, err := github.NewClient(
		github.WithHTTPClient(srv.Client()),
		github.WithURLs(&base, &base),
	)
	assert.NilError(t, err)
	instClient, err := github.NewClient(
		github.WithHTTPClient(srv.Client()),
		github.WithURLs(&base, &base),
	)
	assert.NilError(t, err)
	return appClient, instClient
}

func orgFixture(installationID int64, login string) *github.User {
	return &github.User{Login: github.Ptr(login), Type: github.Ptr("Organization")}
}

func TestVerifyInstallationAccess_OrgMemberAllowed(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	app, inst := newGitHubFixture(t, &ghFixture{
		installationID: installationID,
		account:        orgFixture(installationID, "acme"),
		userByID:       map[int64]string{ghUserID: "alice"},
		membership: map[string]*github.Membership{
			"acme/alice": {State: github.Ptr("active"), User: &github.User{ID: github.Ptr(ghUserID)}},
		},
	})
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "alice"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.NilError(t, err)
}

// A member whose username was renamed/recycled is still resolved by their
// immutable id: the membership endpoint must be queried with the current login,
// not the stale one cached on the user.
func TestVerifyInstallationAccess_StaleUsernameResolvedByID(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	fx := &ghFixture{
		installationID: installationID,
		account:        orgFixture(installationID, "acme"),
		userByID:       map[int64]string{ghUserID: "newlogin"},
		membership: map[string]*github.Membership{
			"acme/newlogin": {State: github.Ptr("active"), User: &github.User{ID: github.Ptr(ghUserID)}},
		},
	}
	app, inst := newGitHubFixture(t, fx)
	// The cached username is stale; only the id is trustworthy.
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "oldlogin"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.NilError(t, err)
	assert.Equal(t, fx.requestedMembershipLogin, "newlogin")
}

func TestVerifyInstallationAccess_NonMemberDenied(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	app, inst := newGitHubFixture(t, &ghFixture{
		installationID: installationID,
		account:        orgFixture(installationID, "acme"),
		userByID:       map[int64]string{ghUserID: "alice"},
		// no membership row -> 404
	})
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "alice"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

// A pending invitation is not active membership.
func TestVerifyInstallationAccess_PendingMembershipDenied(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	app, inst := newGitHubFixture(t, &ghFixture{
		installationID: installationID,
		account:        orgFixture(installationID, "acme"),
		userByID:       map[int64]string{ghUserID: "alice"},
		membership: map[string]*github.Membership{
			"acme/alice": {State: github.Ptr("pending"), User: &github.User{ID: github.Ptr(ghUserID)}},
		},
	})
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "alice"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

// The login resolved to a recycled handle now owned by someone else: the
// membership row is active but its user id does not match the caller.
func TestVerifyInstallationAccess_MembershipUserIDMismatchDenied(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	app, inst := newGitHubFixture(t, &ghFixture{
		installationID: installationID,
		account:        orgFixture(installationID, "acme"),
		userByID:       map[int64]string{ghUserID: "alice"},
		membership: map[string]*github.Membership{
			"acme/alice": {State: github.Ptr("active"), User: &github.User{ID: github.Ptr(int64(9999))}},
		},
	})
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "alice"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestVerifyInstallationAccess_UserAccountOwnerAllowed(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	app, inst := newGitHubFixture(t, &ghFixture{
		installationID: installationID,
		account:        &github.User{ID: github.Ptr(ghUserID), Login: github.Ptr("alice"), Type: github.Ptr("User")},
	})
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "alice"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.NilError(t, err)
}

func TestVerifyInstallationAccess_UserAccountNonOwnerDenied(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	app, inst := newGitHubFixture(t, &ghFixture{
		installationID: installationID,
		account:        &github.User{ID: github.Ptr(int64(2002)), Login: github.Ptr("bob"), Type: github.Ptr("User")},
	})
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "alice"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestVerifyInstallationAccess_InstallationNotFound(t *testing.T) {
	const installationID, ghUserID = int64(42), int64(1001)
	app, inst := newGitHubFixture(t, &ghFixture{installationID: installationID}) // no account -> 404
	user := &model.User{GitHubUserID: ghUserID, GitHubUsername: "alice"}
	err := verifyInstallationAccess(t.Context(), app, inst, installationID, user)
	assert.Equal(t, connect.CodeOf(err), connect.CodeNotFound)
}
