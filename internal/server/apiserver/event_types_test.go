package apiserver

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
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
	assert.DeepEqual(t, attrKeys(issueComment), []string{"body", "url", "mention"})

	// AttrDefs carry display copy sourced from the schema, so the mention attr's
	// label/help/placeholder come through the RPC rather than being hardcoded in
	// the frontend.
	mention := findAttr(issueComment, "mention")
	assert.Assert(t, mention != nil, "expected mention attr on issue_comment")
	assert.Equal(t, mention.Label, "Mention")
	assert.Equal(t, mention.Placeholder, "octocat")
	assert.Assert(t, mention.Help != "", "expected mention attr to carry help text")

	labelAdded, ok := byKey["github:label_added"]
	assert.Assert(t, ok, "expected github/label_added to be registered")
	assert.DeepEqual(t, attrKeys(labelAdded), []string{"body", "url", "label"})

	// The atlassian producer registers too (apiserver imports it), confirming the
	// handler exposes the whole global registry, not just one source.
	_, ok = byKey["atlassian:comment_created"]
	assert.Assert(t, ok, "expected atlassian/comment_created to be registered")
}

// attrKeys extracts the attr keys from an event-type def, in order.
func attrKeys(def *xagentv1.EventTypeDef) []string {
	keys := make([]string, len(def.Attrs))
	for i, a := range def.Attrs {
		keys[i] = a.Key
	}
	return keys
}

// findAttr returns the AttrDef with the given key, or nil.
func findAttr(def *xagentv1.EventTypeDef, key string) *xagentv1.AttrDef {
	for _, a := range def.Attrs {
		if a.Key == key {
			return a
		}
	}
	return nil
}
