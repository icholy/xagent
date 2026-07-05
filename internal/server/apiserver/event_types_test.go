package apiserver

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"

	// Blank-imported so their init registers the eventrouter schemas the
	// handler serves. atlassianserver is already imported transitively by the
	// apiserver package; githubserver is only referenced via an interface, so it
	// must be pulled in explicitly here for its github/* types to be registered.
	_ "github.com/icholy/xagent/internal/server/githubserver"
)

func TestGetEventTypes(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	// Act
	resp, err := srv.GetEventTypes(ctx, &xagentv1.GetEventTypesRequest{})

	// Assert
	assert.NilError(t, err)
	byKey := map[string]*xagentv1.EventTypeDef{}
	for _, et := range resp.EventTypes {
		byKey[et.Source+":"+et.Type] = et
	}

	issueComment, ok := byKey["github:issue_comment"]
	assert.Assert(t, ok, "expected github/issue_comment to be registered")
	assert.Equal(t, issueComment.Label, "GitHub: Issue/PR Comment")

	// AttrDefs carry display copy sourced from the schema, so each attr's
	// label/help/placeholder comes through the RPC rather than being hardcoded in
	// the frontend.
	assert.DeepEqual(t, issueComment.Attrs, []*xagentv1.AttrDef{
		{
			Key:         "body",
			Label:       "Body",
			Placeholder: "xagent:",
			Help:        "Matched against the event body — the comment or description text.",
		},
		{
			Key:         "url",
			Label:       "URL",
			Placeholder: "https://github.com/owner/repo/",
			Help:        "Matched against the event URL — e.g. to scope a rule to a single repo or project.",
		},
		{
			Key:         "mention",
			Label:       "Mention",
			Placeholder: "octocat",
			Help:        "GitHub username mentioned in the event body (no leading @).",
		},
	}, protocmp.Transform())

	labelAdded, ok := byKey["github:label_added"]
	assert.Assert(t, ok, "expected github/label_added to be registered")
	assert.DeepEqual(t, labelAdded.Attrs, []*xagentv1.AttrDef{
		{
			Key:         "body",
			Label:       "Body",
			Placeholder: "xagent:",
			Help:        "Matched against the event body — the comment or description text.",
		},
		{
			Key:         "url",
			Label:       "URL",
			Placeholder: "https://github.com/owner/repo/",
			Help:        "Matched against the event URL — e.g. to scope a rule to a single repo or project.",
		},
		{
			Key:         "label",
			Label:       "Label",
			Placeholder: "xagent",
			Help:        "A label added to the issue or PR.",
		},
	}, protocmp.Transform())

	// The atlassian producer registers too (apiserver imports it), confirming the
	// handler exposes the whole global registry, not just one source.
	_, ok = byKey["atlassian:comment_created"]
	assert.Assert(t, ok, "expected atlassian/comment_created to be registered")
}
