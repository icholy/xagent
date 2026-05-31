package githubserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v88/github"
	"gotest.tools/v3/assert"
)

func TestCreateReviewReaction(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Method, http.MethodPost)
		assert.Equal(t, r.Header.Get("Content-Type"), "application/json")
		body, _ := io.ReadAll(r.Body)
		assert.NilError(t, json.Unmarshal(body, &gotBody))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{"addReaction":{"clientMutationId":null}}}`)
	}))
	defer srv.Close()

	old := githubGraphQLURL
	githubGraphQLURL = srv.URL
	defer func() { githubGraphQLURL = old }()

	client, _ := github.NewClient()
	err := createReviewReaction(context.Background(), client, "PRR_node123", "rocket")
	assert.NilError(t, err)

	vars := gotBody["variables"].(map[string]any)
	assert.Equal(t, vars["subjectId"], "PRR_node123")
	assert.Equal(t, vars["content"], "ROCKET")
}

func TestCreateReviewReaction_UnsupportedContent(t *testing.T) {
	client, _ := github.NewClient()
	err := createReviewReaction(context.Background(), client, "PRR_node123", "heart")
	assert.ErrorContains(t, err, "unsupported reaction content")
}

func TestCreateReviewReaction_GraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"errors":[{"message":"Could not resolve to a node"}]}`)
	}))
	defer srv.Close()

	old := githubGraphQLURL
	githubGraphQLURL = srv.URL
	defer func() { githubGraphQLURL = old }()

	client, _ := github.NewClient()
	err := createReviewReaction(context.Background(), client, "bad_node", "eyes")
	assert.ErrorContains(t, err, "Could not resolve to a node")
}
