package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestWithGRPCMetadata_MapsBearerAPIKeyToXAPIKey(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer sk_test_123")

	ctx := withGRPCMetadata(context.Background(), req)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)

	require.Equal(t, []string{"sk_test_123"}, md.Get("x-api-key"))
	require.Empty(t, md.Get("authorization"))
}

func TestWithGRPCMetadata_ForwardsJWTAuthorization(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer header.payload.signature")

	ctx := withGRPCMetadata(context.Background(), req)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)

	require.Equal(t, []string{"Bearer header.payload.signature"}, md.Get("authorization"))
	require.Empty(t, md.Get("x-api-key"))
}

func TestWithGRPCMetadata_UsesUserContextForIdentityHeaders(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()

	req := httptest.NewRequest("GET", "http://example.com/api/v1/tasks", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.UserContext{
		UserID:   userID,
		TenantID: tenantID,
	}))

	ctx := withGRPCMetadata(context.Background(), req)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)

	require.Equal(t, []string{userID.String()}, md.Get("x-user-id"))
	require.Equal(t, []string{tenantID.String()}, md.Get("x-tenant-id"))
}
