package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestCreateWebhook(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)

	// Act
	resp, err := srv.CreateWebhook(ctx, &xagentv1.CreateWebhookRequest{
		Secret: "my-webhook-secret",
	})

	// Assert
	assert.NilError(t, err)
	assert.Assert(t, resp.Webhook.Uuid != "")
	assert.Assert(t, resp.Webhook.CreatedAt != nil)
}

func TestCreateWebhook_EmptySecret(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)

	// Act
	_, err := srv.CreateWebhook(ctx, &xagentv1.CreateWebhookRequest{
		Secret: "",
	})

	// Assert
	assert.ErrorContains(t, err, "secret is required")
}

func TestGetWebhook(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)
	createResp, err := srv.CreateWebhook(ctx, &xagentv1.CreateWebhookRequest{
		Secret: "my-secret",
	})
	assert.NilError(t, err)

	// Act
	getResp, err := srv.GetWebhook(ctx, &xagentv1.GetWebhookRequest{
		Uuid: createResp.Webhook.Uuid,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, getResp.Webhook.Uuid, createResp.Webhook.Uuid)
}

func TestGetWebhook_NotFound(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)

	// Act
	_, err := srv.GetWebhook(ctx, &xagentv1.GetWebhookRequest{
		Uuid: "00000000-0000-0000-0000-000000000000",
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestGetWebhook_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := randomUserID(t)
	userB := randomUserID(t)
	createResp, err := srv.CreateWebhook(userA, &xagentv1.CreateWebhookRequest{
		Secret: "user-a-secret",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.GetWebhook(userB, &xagentv1.GetWebhookRequest{
		Uuid: createResp.Webhook.Uuid,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListWebhooks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)
	_, err := srv.CreateWebhook(ctx, &xagentv1.CreateWebhookRequest{
		Secret: "secret-1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateWebhook(ctx, &xagentv1.CreateWebhookRequest{
		Secret: "secret-2",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListWebhooks(ctx, &xagentv1.ListWebhooksRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Webhooks), 2)
}

func TestListWebhooks_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := randomUserID(t)
	userB := randomUserID(t)
	_, err := srv.CreateWebhook(userA, &xagentv1.CreateWebhookRequest{
		Secret: "user-a-secret",
	})
	assert.NilError(t, err)
	_, err = srv.CreateWebhook(userB, &xagentv1.CreateWebhookRequest{
		Secret: "user-b-secret",
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListWebhooks(userA, &xagentv1.ListWebhooksRequest{})
	assert.NilError(t, err)
	respB, err := srv.ListWebhooks(userB, &xagentv1.ListWebhooksRequest{})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Webhooks), 1)
	assert.Equal(t, len(respB.Webhooks), 1)
}

func TestDeleteWebhook(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)
	createResp, err := srv.CreateWebhook(ctx, &xagentv1.CreateWebhookRequest{
		Secret: "to-delete",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteWebhook(ctx, &xagentv1.DeleteWebhookRequest{
		Uuid: createResp.Webhook.Uuid,
	})

	// Assert
	assert.NilError(t, err)
	_, err = srv.GetWebhook(ctx, &xagentv1.GetWebhookRequest{
		Uuid: createResp.Webhook.Uuid,
	})
	assert.ErrorContains(t, err, "not found")
}

func TestDeleteWebhook_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := randomUserID(t)
	userB := randomUserID(t)
	createResp, err := srv.CreateWebhook(userA, &xagentv1.CreateWebhookRequest{
		Secret: "user-a-secret",
	})
	assert.NilError(t, err)

	// Act - User B tries to delete User A's webhook
	_, err = srv.DeleteWebhook(userB, &xagentv1.DeleteWebhookRequest{
		Uuid: createResp.Webhook.Uuid,
	})

	// Assert - delete silently fails, webhook still exists for User A
	assert.NilError(t, err)
	_, err = srv.GetWebhook(userA, &xagentv1.GetWebhookRequest{
		Uuid: createResp.Webhook.Uuid,
	})
	assert.NilError(t, err)
}
