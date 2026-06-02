package apiserver

import (
	"context"
	"math/rand/v2"
	"strconv"
	"testing"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// newGitHubUserID returns a positive random GitHub user id. Random rather
// than monotonic so values don't collide with rows left over from previous
// test runs (teststore.CreateOrg destroys the org but not its owner user).
func newGitHubUserID() int64 {
	return rand.Int64N(1_000_000_000) + 1
}

// newInstallationID returns a positive random installation id. Used to avoid
// (type, external_id) PK collisions in pending_integrations and the unique
// index on orgs.github_installation_id across parallel and repeated runs.
func newInstallationID() int64 {
	return rand.Int64N(1_000_000_000) + 1
}

// setupGitHubLinkable creates an org whose owner has a linked GitHub account
// and seeds a matching pending_integration row. If senderGitHubUserID is 0 it
// defaults to the owner's GitHub user id so the link call succeeds.
func setupGitHubLinkable(t *testing.T, srv *Server, installationID, senderGitHubUserID int64) *teststore.Org {
	t.Helper()
	org := teststore.CreateOrg(t, srv.store, nil)
	ownerGitHubID := newGitHubUserID()
	assert.NilError(t, srv.store.LinkGitHubAccount(t.Context(), nil, org.UserID, ownerGitHubID, "owner"))
	if senderGitHubUserID == 0 {
		senderGitHubUserID = ownerGitHubID
	}
	pending := &model.PendingIntegration{
		Type:       model.PendingIntegrationTypeGitHub,
		ExternalID: strconv.FormatInt(installationID, 10),
		Options: model.PendingIntegrationOptions{
			GitHub: &model.GitHubPendingIntegration{
				SenderGitHubUserID: senderGitHubUserID,
				AccountLogin:       "acme",
				AccountType:        "Organization",
			},
		},
	}
	assert.NilError(t, srv.store.UpsertPendingIntegration(t.Context(), nil, pending))
	return org
}

func TestLinkGitHubInstallation(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	installationID := newInstallationID()
	org := setupGitHubLinkable(t, srv, installationID, 0)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: installationID,
	})
	assert.NilError(t, err)

	got, err := srv.store.GetOrg(ctx, nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, got.GitHubInstallationID, installationID)

	// Pending row should have been consumed.
	_, err = srv.store.GetPendingIntegration(ctx, nil, model.PendingIntegrationTypeGitHub, strconv.FormatInt(installationID, 10))
	assert.ErrorContains(t, err, "no rows")
}

func TestLinkGitHubInstallation_MissingID(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{})
	assert.Equal(t, connect.CodeOf(err), connect.CodeInvalidArgument)
}

func TestLinkGitHubInstallation_Unauthenticated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})

	_, err := srv.LinkGitHubInstallation(context.Background(), &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: newInstallationID(),
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeUnauthenticated)
}

func TestLinkGitHubInstallation_NoLinkedGitHubAccount(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: newInstallationID(),
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeFailedPrecondition)
}

func TestLinkGitHubInstallation_NoPendingRow(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	assert.NilError(t, srv.store.LinkGitHubAccount(t.Context(), nil, org.UserID, newGitHubUserID(), "owner"))
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: newInstallationID(),
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodeNotFound)
}

func TestLinkGitHubInstallation_SenderMismatch(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	// Pending row was started by a different GitHub user than the caller.
	otherSender := newGitHubUserID()
	installationID := newInstallationID()
	org := setupGitHubLinkable(t, srv, installationID, otherSender)
	ctx := createCtx(t, org)

	_, err := srv.LinkGitHubInstallation(ctx, &xagentv1.LinkGitHubInstallationRequest{
		InstallationId: installationID,
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
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
	installationID := newInstallationID()
	org := setupGitHubLinkable(t, srv, installationID, 0)
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
