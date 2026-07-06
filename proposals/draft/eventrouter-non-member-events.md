# Eventrouter: support events from non-org-member users

Issue: https://github.com/icholy/xagent/issues/1255

## Problem

The eventrouter only routes an external event when the user who performed the
action (the GitHub commenter, the Jira commenter, the labeler, etc.) is an
**oauth-linked member** of an xagent org. This falls out of two gates:

1. **Webhook handlers drop unlinked actors.** Both the GitHub and Atlassian
   handlers resolve the acting user to an xagent user before routing —
   `GetUserByGitHubUserID` (`internal/server/githubserver/webhook.go:53`) and
   `GetUserByAtlassianAccountID` (`internal/server/atlassianserver/webhook.go:81`).
   If the actor has no linked account, the handler logs `no linked account` and
   returns without routing.

2. **The router only considers the actor's member orgs.** `Router.Route`
   short-circuits when `input.UserID == ""`
   (`internal/eventrouter/eventrouter.go:97`) and then fetches rules via
   `ListRoutingRulesForUser`, whose SQL `JOIN org_members` restricts results to
   orgs the actor belongs to (`internal/store/sql/queries/org.sql:89-93`).

The result: to trigger a rule, the actor must have oauth-linked their GitHub and
Jira accounts to xagent **and** be a member of the org. We want the option to
create rules that fire for **non-member** actors — for example, an external
contributor commenting `@xagent-bot` on a PR, or an outside reporter labeling an
issue — without every such actor having to link accounts and join the org.

Non-member routing must be **opt-in per rule**, so the change never widens who
can trigger an existing rule. Only rules whose author explicitly enabled the new
toggle become reachable by non-members.

## Design

Two new pieces, exactly as sketched in the issue:

1. A new `Orgs []int64` field on `eventrouter.InputEvent`, populated by the
   integrations, naming the orgs the event belongs to **independent of the
   actor's membership**.
2. A new per-rule opt-in, `AllowNonMembers`, surfaced as a toggle in the rule
   editor UI.

The router combines them: an org's rules are eligible for an event if the actor
is a **member** of that org (existing behavior — all the org's rules apply), or
if the org appears in `input.Orgs` and the rule has `AllowNonMembers = true`.

### `eventrouter.InputEvent.Orgs`

Add the field to the struct in `internal/eventrouter/eventrouter.go`:

```go
type InputEvent struct {
	Source      string
	Type        string
	Description string
	Data        string
	URL         string
	UserID      string
	// Orgs names the orgs this event belongs to, resolved by the webhook
	// handler independent of the actor's membership (GitHub: the orgs sharing
	// the App installation; Atlassian: the org in the webhook's ?org= param).
	// It gates non-member routing: a rule with AllowNonMembers can fire for one
	// of these orgs even when UserID is empty or the actor is not a member.
	Orgs  []int64
	Attrs Attrs
	Meta  any
}
```

`UserID` becomes optional: with non-member routing, an event may route with no
linked actor at all.

### `RoutingRule.AllowNonMembers`

**Proto** (`proto/xagent/v1/xagent.proto`). The next free field number is `11`
(reserved: `3,4,6,7,8`; used: `1,2,5,9,10`):

```protobuf
message RoutingRule {
  reserved 3, 4, 6, 7, 8;
  string source = 1;
  string type = 2;
  CreateTaskAction create = 5;
  bool wakeup = 9;
  repeated RuleCondition conditions = 10;
  // allow_non_members lets this rule fire for actors who are not members of the
  // org (and need not be oauth-linked). Defaults false — rules are member-only
  // unless explicitly opted in.
  bool allow_non_members = 11;
}
```

**Model** (`internal/model/routing_rule.go`). Add the field and plumb it through
`Proto()` / `RoutingRuleFromProto()`:

```go
type RoutingRule struct {
	Source          string            `json:"source,omitempty"`
	Type            string            `json:"type,omitempty"`
	Conditions      []Condition       `json:"conditions,omitempty"`
	Create          *CreateTaskAction `json:"create,omitempty"`
	Wakeup          bool              `json:"wakeup,omitempty"`
	AllowNonMembers bool              `json:"allow_non_members,omitempty"`
}
```

Rules are stored as JSONB in `orgs.routing_rules`, so the new field needs no
schema migration — existing rows decode with `AllowNonMembers = false`, which is
the safe default (member-only).

### Router: combine member and non-member orgs

`Router.Route` gains a non-member augmentation step. Today it fetches only the
actor's member orgs; now it also considers orgs from `input.Orgs`, restricting
those to rules that opted in.

```go
func (r *Router) Route(ctx context.Context, input InputEvent) (int, error) {
	if input.URL == "" {
		return 0, nil
	}

	// Member orgs: the actor's orgs contribute all their rules (existing
	// behavior). Skip the query when there's no linked actor.
	rulesByOrg := map[int64][]model.RoutingRule{}
	if input.UserID != "" {
		var err error
		rulesByOrg, err = r.Store.ListRoutingRulesForUser(ctx, nil, input.UserID)
		if err != nil {
			return 0, err
		}
	}

	// Non-member orgs: orgs named on the event that the actor is NOT already a
	// member of contribute only their AllowNonMembers rules.
	var extraOrgs []int64
	for _, orgID := range input.Orgs {
		if _, ok := rulesByOrg[orgID]; !ok {
			extraOrgs = append(extraOrgs, orgID)
		}
	}
	if len(extraOrgs) > 0 {
		extra, err := r.Store.GetRoutingRulesByOrgs(ctx, nil, extraOrgs)
		if err != nil {
			return 0, err
		}
		for orgID, rules := range extra {
			filtered := rules[:0:0]
			for _, rule := range rules {
				if rule.AllowNonMembers {
					filtered = append(filtered, rule)
				}
			}
			if len(filtered) > 0 {
				rulesByOrg[orgID] = filtered
			}
		}
	}

	// ... unchanged from here: default-rule fallback per org, Match, link
	// lookup, wake/create.
}
```

Key properties:

- **Member orgs keep today's semantics** — all rules apply, and the ruleless-org
  `reg.DefaultRules()` fallback (the `xagent:` body-prefix wakeup defaults) still
  runs. That fallback is intentionally **not** applied to non-member orgs: the
  default rules are member-only, so a ruleless non-member org routes nothing.
  Non-member routing always requires an explicit opt-in rule.
- **Overlap resolves in favor of membership.** An org that is both a member org
  and in `input.Orgs` is populated by the member query first and skipped by the
  `extraOrgs` filter, so all its rules apply — a member is never down-scoped to
  the non-member rule subset.
- **`GetRoutingRulesByOrgs` already exists** (`internal/store/sql/queries/org.sql:86`)
  and returns `map[int64][]model.RoutingRule` for a set of org ids — no new store
  method is needed on the router side.

The old early-return on `input.UserID == ""` is replaced by the `input.URL == ""`
guard plus the conditional member query, so an event with an empty `UserID` but a
non-empty `Orgs` still routes.

### Integrations populate `Orgs`

**Atlassian** (`internal/server/atlassianserver/webhook.go`). The org is already
known — it's the `?org=` query parameter the handler parses and validates. Set
it on the event and stop dropping unlinked actors:

```go
input.Orgs = []int64{orgID}

user, err := h.Store.GetUserByAtlassianAccountID(r.Context(), nil, meta.AuthorAccountID)
switch {
case err == nil:
	input.UserID = user.Id
case errors.Is(err, sql.ErrNoRows):
	// Unlinked actor: leave UserID empty and let the router decide via
	// AllowNonMembers rules for input.Orgs.
default:
	// ... internal error
}

routed, err := h.Router.Route(r.Context(), *input)
```

**GitHub** (`internal/server/githubserver/webhook.go`). GitHub webhooks don't
carry an xagent org, but they do carry the App installation, and orgs record
their `github_installation_id` (shared across orgs since migration
`20260621000001_share_github_installation.sql`). Resolve the installation to org
ids:

- Add `InstallationID int64` to `GitHubMeta` and set it from
  `event.GetInstallation().GetID()` in each `toInputEvent` case (mirroring how
  `AuthorID` is already carried on `GitHubMeta`).
- Add a store query to map an installation to its (non-archived) orgs:

  ```sql
  -- name: ListOrgIDsByGitHubInstallation :many
  SELECT id FROM orgs
  WHERE github_installation_id = $1 AND archived = FALSE;
  ```

- In the handler, populate `Orgs` from the installation and stop dropping
  unlinked actors:

  ```go
  input.Orgs, err = h.Store.ListOrgIDsByGitHubInstallation(r.Context(), nil, meta.InstallationID)
  // ... handle error

  user, err := h.Store.GetUserByGitHubUserID(r.Context(), nil, meta.AuthorID)
  switch {
  case err == nil:
      input.UserID = user.ID
      // (existing cached-username refresh stays here)
  case errors.Is(err, sql.ErrNoRows):
      // Unlinked actor: route via AllowNonMembers rules for input.Orgs.
  default:
      // ... internal error
  }

  totalRouted, err := h.Router.Route(r.Context(), *input)
  ```

Restricting non-member routing to the installation's orgs is deliberate: a
non-member event can only reach orgs that have actually installed the GitHub App
on the repo the event came from, not arbitrary orgs.

### Web UI: the toggle

`RoutingRuleFormValues` (`webui/src/lib/routing-rules.ts`) gains an
`allowNonMembers: boolean` field (default `false` in `emptyRoutingRule`), wired
through `formValuesFromRoutingRule` and `buildRoutingRule`. The rule editor
(`webui/src/components/routing-rule-form.tsx`) gains a checkbox next to the
existing **Wake up** toggle, labeled e.g. *"Allow non-members"* with help text
explaining that the rule can then be triggered by users who are not org members
and need not have linked their GitHub/Jira accounts.

## Implementation Plan

An ordered stack of small PRs. Each foundational layer is safe to merge before
the ones above it land — the field simply goes unused until the router and
handlers consume it.

1. **Proto + model field** — Delivers: `allow_non_members` on the `RoutingRule`
   proto (field 11) plus regenerated code, and `AllowNonMembers` on
   `model.RoutingRule` plumbed through `Proto()` / `RoutingRuleFromProto()`.
   Depends on: nothing. Verifiable by: `mise run generate` produces a clean
   diff; a model round-trip unit test (`model → proto → model`) preserves the
   flag.

2. **`InputEvent.Orgs` + router non-member routing** — Delivers: the `Orgs`
   field on `InputEvent`, the relaxed entry guard, and the non-member
   augmentation (fetch `extraOrgs` via `GetRoutingRulesByOrgs`, keep only
   `AllowNonMembers` rules, don't fall back to default rules for them). Depends
   on: (1). Verifiable by: eventrouter unit tests — a member still matches all
   rules; a non-member with `Orgs` matches only an `AllowNonMembers` rule; an
   empty `UserID` with `Orgs` routes; a member org that also appears in `Orgs`
   still uses its full rule set; a ruleless non-member org routes nothing.

3. **Store: orgs by GitHub installation** — Delivers: the
   `ListOrgIDsByGitHubInstallation` query and store method (non-archived only).
   Depends on: nothing (independently mergeable). Verifiable by: a store unit
   test over orgs sharing an installation id, excluding archived orgs.

4. **GitHub webhook wire-up** — Delivers: `InstallationID` on `GitHubMeta`, the
   handler populating `input.Orgs` from the installation, and the change from
   dropping unlinked actors to routing them (empty `UserID`). Depends on: (2),
   (3). Verifiable by: webhook handler tests — a linked member routes as before;
   an unlinked actor routes only through an `AllowNonMembers` rule on an org
   sharing the installation.

5. **Atlassian webhook wire-up** — Delivers: the handler setting
   `input.Orgs = []int64{orgID}` and routing unlinked actors instead of dropping
   them. Depends on: (2). Verifiable by: webhook handler tests mirroring the
   GitHub cases, using the `?org=` param.

6. **Web UI toggle** — Delivers: `allowNonMembers` in the form values/build/parse
   helpers and the checkbox in the rule editor. Depends on: (1) (consumes the
   regenerated proto). Verifiable by: `pnpm lint` in `webui/` and rendering the
   editor against a rule with the flag set/unset.

## Trade-offs

- **Member routing stays actor-scoped; non-member routing is org/installation-scoped.**
  A linked member's event continues to route to *all* their member orgs
  regardless of which installation/repo produced it (today's behavior, left
  unchanged). Non-member routing is narrower — it only reaches orgs named in
  `input.Orgs` (the installation's orgs for GitHub, the `?org=` org for
  Atlassian). This asymmetry keeps the change strictly additive: no existing
  member's routing behavior changes, and non-members are confined to orgs that
  demonstrably own the integration the event came from.

- **Opt-in per rule rather than per org.** A per-org "allow non-members" setting
  would be simpler to store but coarser — it would open *every* rule in the org
  to non-members. Per-rule keeps the blast radius tight: an org can, say, accept
  non-member `@bot` mentions on PRs while still gating issue-label rules to
  members. Reusing the existing JSONB rule shape also avoids a schema migration.

- **Reusing `GetRoutingRulesByOrgs` vs. a bespoke query.** The router could push
  the `AllowNonMembers` filter into SQL, but the existing method already returns
  the rules per org and the filter is a trivial in-memory pass; adding a
  specialized query buys little.

## Open Questions

- **Should member routing also intersect with `input.Orgs`?** Currently a linked
  member routes to all their orgs even if the event's installation isn't tied to
  some of them. Scoping member routing to `input.Orgs` too would be more
  consistent, but it changes long-standing behavior and risks breaking existing
  setups where a member relies on cross-installation routing. Proposed: leave
  member routing unchanged for now.

- **De-duplication when an actor is both a member and matches a non-member rule
  in the same org.** Handled by the `extraOrgs` skip (membership wins), but worth
  confirming there's no path where the same org is processed twice.

- **Archived-org safety for Atlassian.** The GitHub installation query filters
  `archived = FALSE`; the Atlassian handler takes the org straight from `?org=`.
  Should the router (or handler) drop archived orgs from `input.Orgs` centrally
  so no integration can route into an archived org? Proposed: filter in
  `GetRoutingRulesByOrgs`'s caller or add an `archived = FALSE` guard there.

- **UI discoverability / safety.** Should enabling **Allow non-members** show a
  warning, given it widens who can trigger the rule (and, for create-task rules,
  who can cause a container to spin up)? Rate-limiting or restricting
  non-member create-task rules is out of scope here but worth flagging.
