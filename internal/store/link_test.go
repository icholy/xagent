package store_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestCreateAndListLinksRoutingKey(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	link := &model.Link{
		TaskID:     task.ID,
		Relevance:  "trigger",
		URL:        "https://github.com/o/r/pull/5#issuecomment-9",
		RoutingKey: "https://github.com/o/r/pull/5",
		Title:      "PR",
		Subscribe:  true,
		CreatedAt:  time.Now(),
	}
	err := s.CreateLink(t.Context(), nil, link)
	assert.NilError(t, err)

	// Act
	links, err := s.ListLinksByTask(t.Context(), nil, task.ID, org.OrgID)

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(links), 1)
	assert.DeepEqual(t, links[0], link, cmpopts.IgnoreFields(model.Link{}, "CreatedAt"))
}

func TestFindSubscribedLinksForOrgsMatchesRoutingKey(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	link := &model.Link{
		TaskID:     task.ID,
		Relevance:  "trigger",
		URL:        "https://github.com/o/r/pull/5#issuecomment-9",
		RoutingKey: "https://github.com/o/r/pull/5",
		Subscribe:  true,
		CreatedAt:  time.Now(),
	}
	assert.NilError(t, s.CreateLink(t.Context(), nil, link))

	// Act - matching on the routing key finds the link
	byRouting, err := s.FindSubscribedLinksForOrgs(t.Context(), nil, "https://github.com/o/r/pull/5", []int64{org.OrgID})
	assert.NilError(t, err)
	// matching on the expressive URL does not
	byURL, err := s.FindSubscribedLinksForOrgs(t.Context(), nil, "https://github.com/o/r/pull/5#issuecomment-9", []int64{org.OrgID})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(byRouting[org.OrgID]), 1)
	assert.Equal(t, byRouting[org.OrgID][0].ID, link.ID)
	assert.Equal(t, byRouting[org.OrgID][0].RoutingKey, "https://github.com/o/r/pull/5")
	assert.Equal(t, len(byURL[org.OrgID]), 0)
}
