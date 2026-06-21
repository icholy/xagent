# Share a GitHub Installation Across Multiple xagent Orgs

Issue: https://github.com/icholy/xagent/issues/1019

## Problem

The GitHub integration enforces a **1-to-1 mapping between an xagent org and a GitHub App installation**. Two coworkers who each own a separate xagent org but share one work GitHub org cannot both use the integration: only the first to link wins, and the second is locked out.

Two mechanisms tie an installation to a single org today:

1. **A unique index** — `orgs.github_installation_id BIGINT` + `CREATE UNIQUE INDEX idx_orgs_github_installation_id ON orgs(github_installation_id)` (`internal/store/sql/migrations/20260517000001_github_installation.sql`). A second org physically cannot store the same `installation_id`.
2. **The claim-flow authorization** — `LinkGitHubInstallation` requires the linking user to be the GitHub user who initiated the install (`pending.Options.GitHub.SenderGitHubUserID != user.GitHubUserID → PermissionDenied`) and consumes the single-use pending row on first link (`internal/server/apiserver/github.go:50-58`).

The blast radius is narrow. Inbound webhook routing **does not use the installation linkage at all** — it looks up the xagent user by GitHub *author* ID and routes to that user's org memberships and routing rules (`internal/server/githubserver/webhook.go:55-76`, `internal/eventrouter/eventrouter.go`). So a coworker's GitHub activity already routes into their own org. The `github_installation_id` is consumed in only two places:

- **Emoji reactions** — `react()` loads the matched org and uses `org.GitHubInstallationID` to mint an installation token and add 🚀/👀 (`internal/server/githubserver/reactions.go:42-53`). An org with no installation silently gets no reactions.
- **Claim/uninstall bookkeeping** — `SetOrgGitHubInstallation` / `ClearGitHubInstallation` and the `GetOrgSettings` display field.

Agent-side GitHub tokens are unaffected: per #806 those come from user-owned credentials in the runner proxy, not the App installation.

So the felt experience of the bug for the second coworker is: routing works (tasks fire), but Settings perpetually shows "not installed" and they get no emoji reactions. They cannot reach the "Installed" state because the app is already installed on the shared org (no fresh `installation.created` webhook → no pending row to claim), and even if there were a pending row the sender check and unique index would block the write.

## Design

Drop the unique constraint so multiple orgs can point at the same installation, and replace the "must be the original installer" claim check with a **verified-membership check performed server-side with the App's own token**. No new tables, no user-facing re-consent, no stored user OAuth tokens.

### 1. Schema: make the index non-unique

The column stays; only the uniqueness goes away. `ClearGitHubInstallation` already clears *by installation id* across all matching rows (`internal/store/sql/queries/org.sql`), so uninstall cleanup keeps working for every org sharing the installation.

```sql
-- internal/store/sql/migrations/<timestamp>_share_github_installation.sql
-- migrate:up
DROP INDEX IF EXISTS idx_orgs_github_installation_id;
CREATE INDEX idx_orgs_github_installation_id ON orgs(github_installation_id);

-- migrate:down
DROP INDEX IF EXISTS idx_orgs_github_installation_id;
CREATE UNIQUE INDEX idx_orgs_github_installation_id ON orgs(github_installation_id);
```

No query changes: `SetOrgGitHubInstallation`, `ClearGitHubInstallation`, and `GetOrg` are unchanged. `orgs` keeps a single `github_installation_id` column (this supports **installation → many orgs**, the reported problem; **org → many installations** remains a separate future feature that would need a junction table).

### 2. GitHub App permission: add "Organization members: read"

Verifying membership with an installation token (`GET /orgs/{org}/members/{username}`) requires the App to hold the organization **Members** read permission. The App does not need it today (reactions and installation tokens only touch issues/PRs + metadata), so it must be added in the GitHub App settings.

Adding an org permission triggers a re-approval prompt to every existing installation. **There is exactly one installation today (the maintainer's), so this is a single one-time re-approval** with no fleet to migrate. Every coworker who links afterward inherits the permission — they never see this prompt.

### 3. Authorization: verify membership with the App token

Replace the pending-row sender check in `LinkGitHubInstallation` with a membership check resolved entirely server-side. The caller's GitHub identity is already verified at login (`LinkGitHubAccount` stores a GitHub-confirmed `GitHubUserID`/`GitHubUsername`, `internal/server/githubserver/githubserver.go:148-157`), so there is no impersonation risk in checking "is this verified user a member of the installation's account?".

Add a method to `githubserver.Server`, which already holds the App JWT transport (`s.app`) and the installation-token cache (`s.tokens`):

```go
// internal/server/githubserver/installations.go

// VerifyInstallationAccess returns nil if the user is allowed to link the given
// installation: a member of the installation's organization, or the owner of a
// user-account installation. Returns a connect PermissionDenied / NotFound error
// otherwise.
func (s *Server) VerifyInstallationAccess(ctx context.Context, installationID int64, user *model.User) error {
    // App JWT identifies the installation's account (login + type). Don't rely on
    // the pending row — it won't exist for a second org claiming an existing install.
    appClient := github.NewClient(&http.Client{Transport: s.app})
    inst, _, err := appClient.Apps.GetInstallation(ctx, installationID)
    if err != nil { /* NotFound if 404, else Internal */ }

    switch inst.GetAccount().GetType() {
    case "Organization":
        // Installation token + Members:read. 404 => not a member.
        instClient := github.NewClient(s.tokens.Client(installationID))
        m, _, err := instClient.Organizations.GetOrgMembership(ctx, user.GitHubUsername, inst.GetAccount().GetLogin())
        if isNotFound(err) || m.GetState() != "active" {
            return connect.NewError(connect.CodePermissionDenied, errors.New("you are not a member of this GitHub organization"))
        }
        return err // nil on success
    case "User":
        // No members concept for a personal account: only the owner may link it.
        if user.GitHubUserID != inst.GetAccount().GetID() {
            return connect.NewError(connect.CodePermissionDenied, errors.New("this installation belongs to a different GitHub user"))
        }
        return nil
    }
    return connect.NewError(connect.CodePermissionDenied, errors.New("unsupported installation account type"))
}
```

`LinkGitHubInstallation` (`internal/server/apiserver/github.go`) — which already holds a `*githubserver.Server` (`s.github`) — becomes:

```go
user := s.store.GetUser(ctx, nil, caller.ID)
if !user.HasGitHub() {
    return FailedPrecondition("link your GitHub account first at /github/login")
}
if err := s.github.VerifyInstallationAccess(ctx, req.InstallationId, user); err != nil {
    return err
}
if err := s.store.SetOrgGitHubInstallation(ctx, nil, caller.OrgID, req.InstallationId); err != nil {
    return err
}
// Best-effort: clear the pending row if the original installer is claiming it.
// No longer the authorization mechanism, so a missing row is not an error.
_ = s.store.DeletePendingIntegration(ctx, nil, model.PendingIntegrationTypeGitHub, externalID)
```

The membership check is a network round-trip, so it moves **out** of the DB transaction. The transaction that previously guarded the pending-row read/write against a racing webhook (`github.go:30-32`) is no longer needed: the pending row is no longer the source of truth.

With this alone, the **existing redirect flow already works for the second coworker**: Settings → "Install GitHub App" → GitHub shows the app is already installed → "Configure" → save → redirect to `/github/setup?installation_id=…` (GitHub sends `setup_action=update`) → **Approve** → membership verified → linked. The `github.setup.tsx` page needs no change; it stops erroring with "no pending GitHub installation".

### 4. Nicer UX: discover linkable installations (no GitHub bounce)

Because the server can enumerate the App's installations (`GET /app/installations` via `s.app`) and check membership, the coworker doesn't need to round-trip through GitHub's "Configure" page at all. Add an RPC:

```proto
// proto/xagent/v1/xagent.proto
rpc ListLinkableGitHubInstallations(ListLinkableGitHubInstallationsRequest)
    returns (ListLinkableGitHubInstallationsResponse);

message LinkableGitHubInstallation {
  int64  installation_id = 1;
  string account_login   = 2;  // e.g. "work-org"
  string account_type    = 3;  // "Organization" | "User"
  bool   linked_to_current_org = 4;
}
message ListLinkableGitHubInstallationsResponse {
  repeated LinkableGitHubInstallation installations = 1;
}
```

Implementation: list all App installations, run `VerifyInstallationAccess` against the caller for each, return the ones that pass (annotating those already linked to the current org via the org's `github_installation_id`). With one installation this is a single membership check.

The Settings "GitHub App" card (`webui/src/routes/settings.tsx:193-230`) then renders, for an org not yet linked:

> ✓ You're a member of **work-org**, which has the GitHub App installed. **[Link to this org]**

The button calls `LinkGitHubInstallation` directly — one click, no bounce to GitHub, no pending row. The existing "Install GitHub App" link + `/github/setup` flow remains as the path for installing the App somewhere new (when the caller has no linkable installation yet).

### Reactions are unchanged

`react()` keeps reading `org.GitHubInstallationID` (`reactions.go:46-53`). Each org still has its own column pointing at the shared installation, so once both orgs are linked, both get reactions. No change to reaction logic is required; resolving the installation from the webhook payload instead (issue's Approach B) is an orthogonal simplification, not needed here.

## Trade-offs

**App token vs. user token for the membership check.** The alternative is verifying with the linking user's own OAuth token via `GET /user/installations`. That answers the question directly and needs no new App permission, but the OAuth login flow currently requests only `read:user` and **discards the token** (`githubserver.go:130,142-157`). Using it would require either a re-consent at link time or persisting user GitHub tokens. The App-token path needs a one-time `Members:read` approval (trivial at one installation) but then verifies every future coworker with zero user-facing friction and no new secrets at rest. Given the single existing installation, the App-token path is strictly cheaper.

**Drop the unique index vs. a join table (issue's Approach C).** A junction table `org_github_installations(org_id, installation_id)` is the fully general model and also enables one-org-to-many-GitHub-orgs. It is also the largest change (schema, queries, settings RPC returning a list, UI). Dropping the unique index solves the reported direction (one installation, many orgs) with a one-line migration and no new queries. If one-org-many-installations becomes a real requirement, the join table is the right follow-up; this proposal does not foreclose it.

**Replace the sender check entirely vs. keep it as a fast path.** Keeping the sender check as a fast path for the original installer adds a branch and two code paths for one behavior. The membership check is uniform and covers the original installer too (they are a member/owner), so the sender check is removed outright and the pending row demoted to best-effort cleanup.

## Open Questions

1. **Member vs. admin.** Should any *active* org member be allowed to link, or only **admins/owners**? `GetOrgMembership` returns the role, so requiring `role == "admin"` is a one-line change. Member-level is proposed (matches "coworkers share the integration"); admin-only is more conservative.
2. **Username staleness.** The membership check uses `user.GitHubUsername`, which is refreshed on inbound webhooks (`webhook.go:68-71`) but could be stale at link time after a rename. GitHub's membership endpoint is keyed by username, not ID. Acceptable, or re-fetch the login by ID first?
3. **Scope of this PR.** Is the discovery RPC + Settings affordance (section 4) in scope, or should the first PR be just sections 1-3 (drop index + membership-verified link via the existing `/github/setup` redirect), with the nicer UX as a follow-up?
4. **Reaction noise when a user is in multiple of their own orgs.** A single author's comment can route to several orgs that user belongs to, firing `react()` once per matched org. GitHub reactions are idempotent per app identity, so duplicates collapse to one 🚀/👀 — no change needed, noted for completeness.
