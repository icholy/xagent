package apiserver

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// fakeGitHub is a stand-in for *githubserver.Server in apiserver tests: it
// records the installation ids passed to VerifyInstallationAccess and returns a
// canned error, so LinkGitHubInstallation can be exercised without a real
// GitHub App.
type fakeGitHub struct {
	verifyErr error
	calls     []int64
}

func (f *fakeGitHub) AppInstallURL() string { return "" }

func (f *fakeGitHub) VerifyInstallationAccess(ctx context.Context, installationID int64, user *model.User) error {
	f.calls = append(f.calls, installationID)
	return f.verifyErr
}

// newGitHubUserID returns a positive random GitHub user id. Random rather
// than monotonic so values don't collide with rows left over from previous
// test runs (teststore.CreateOrg destroys the org but not its owner user).
func newGitHubUserID() int64 {
	return rand.Int64N(1_000_000_000) + 1
}

// newInstallationID returns a positive random installation id.
func newInstallationID() int64 {
	return rand.Int64N(1_000_000_000) + 1
}

// setupGitHubLinkable creates an org whose owner has a linked GitHub account.
func setupGitHubLinkable(t *testing.T, srv *Server) *teststore.Org {
	t.Helper()
	org := teststore.CreateOrg(t, srv.store, nil)
	assert.NilError(t, srv.store.LinkGitHubAccount(t.Context(), nil, org.UserID, newGitHubUserID(), "owner"))
	return org
}

func TestLinkGitHubInstallation(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	gh := &fakeGitHub{}
	srv.github = gh
	installationID := newInstallationID()
	org := setupGitHubLinkable(t, srv)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: installationID,
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, gh.calls, []int64{installationID})

	got, err := srv.store.GetOrg(ctx, nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, got.GitHubInstallationID, installationID)
}

// A second org may link an installation already linked to another org: the
// membership check, not a unique index, is what gates linking now.
func TestLinkGitHubInstallation_Shared(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	srv.github = &fakeGitHub{}
	installationID := newInstallationID()

	org1 := setupGitHubLinkable(t, srv)
	_, err := srv.LinkGitHubInstallation(createCtx(t, org1), &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: installationID,
	})
	assert.NilError(t, err)

	org2 := setupGitHubLinkable(t, srv)
	_, err = srv.LinkGitHubInstallation(createCtx(t, org2), &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: installationID,
	})
	assert.NilError(t, err)

	for _, o := range []*teststore.Org{org1, org2} {
		got, err := srv.store.GetOrg(t.Context(), nil, o.OrgID)
		assert.NilError(t, err)
		assert.Equal(t, got.GitHubInstallationID, installationID)
	}
}

func TestLinkGitHubInstallation_MissingID(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	srv.github = &fakeGitHub{}
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{})
	assert.Equal(t, connect.CodeOf(err), connect.CodeInvalidArgument)
}

func TestLinkGitHubInstallation_Unauthenticated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	srv.github = &fakeGitHub{}

	_, err := srv.LinkGitHubInstallation(context.Background(), &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: newInstallationID(),
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeUnauthenticated)
}

func TestLinkGitHubInstallation_NotConfigured(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := setupGitHubLinkable(t, srv)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: newInstallationID(),
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeFailedPrecondition)
}

func TestLinkGitHubInstallation_NoLinkedGitHubAccount(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	srv.github = &fakeGitHub{}
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: newInstallationID(),
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeFailedPrecondition)
}

// A failed membership check propagates as-is (the fake returns PermissionDenied).
func TestLinkGitHubInstallation_AccessDenied(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	srv.github = &fakeGitHub{
		verifyErr: connect.NewError(connect.CodePermissionDenied, errors.New("not a member")),
	}
	org := setupGitHubLinkable(t, srv)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: newInstallationID(),
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	// The installation must not be linked when the check fails.
	got, err := srv.store.GetOrg(ctx, nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, got.GitHubInstallationID, int64(0))
}

// CreateGitHubToken no longer mints tokens on the server; it always reports
// Unimplemented. The real implementation now lives in the runner proxy (#806).
func TestCreateGitHubToken_Unimplemented(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	_, err := srv.CreateGitHubToken(ctx, &xagentv1.CreateGitHubTokenRequest{})
	assert.Equal(t, connect.CodeOf(err), connect.CodeUnimplemented)
}

// Sanity check: the store's clear method removes a previously set installation.
func TestStoreClearGitHubInstallation(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	srv.github = &fakeGitHub{}
	installationID := newInstallationID()
	org := setupGitHubLinkable(t, srv)
	ctx := createCtx(t, org)
	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: installationID,
	})
	assert.NilError(t, err)

	assert.NilError(t, srv.store.ClearGitHubInstallation(ctx, nil, installationID))

	got, err := srv.store.GetOrg(ctx, nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, got.GitHubInstallationID, int64(0))
}
