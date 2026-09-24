package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPOAuthPostgres covers the Postgres side of migration 000018 and the
// store methods behind the MCP server: dynamic clients with no creator,
// include_personal on codes and tokens, the connected-apps listing, and
// page content search. The protocol itself is tested end to end over
// memstore in cmd/api/mcp_test.go.
func TestMCPOAuthPostgres(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)
	ctx := context.Background()
	alice := s.createUser(t, "sub-alice-mcp", "alice-mcp@test.com", "Alice")

	var client *model.OAuthClient
	t.Run("DynamicClientHasNoCreator", func(t *testing.T) {
		w := s.doJSON(t, "POST", "/oauth/register", reqOpts{jsonVal: map[string]interface{}{
			"client_name": "Claude Code", "redirect_uris": []string{"http://localhost:5555/callback"},
		}})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		body := decodeOAuth[map[string]interface{}](t, w)

		var err error
		client, _, err = s.clients.GetByClientID(ctx, body["client_id"].(string))
		require.NoError(t, err)
		assert.True(t, client.IsDynamic)
		assert.Equal(t, uuid.Nil, client.CreatedByID)
		assert.Empty(t, client.OrgIDs)
		assert.Equal(t, []model.OAuthGrantType{model.GrantAuthorizationCode}, client.GrantTypes)
		assert.Contains(t, client.Scopes, model.ScopeLaneRead)
		assert.False(t, client.IsConfidential)

		// The constraint still forbids an ordinary client without a creator.
		_, err = s.pool.Exec(ctx, `INSERT INTO oauth_clients (client_id, client_secret_hash, name) VALUES ('x', 'h', 'n')`)
		assert.Error(t, err)
	})

	t.Run("IncludePersonalRoundTrips", func(t *testing.T) {
		require.NotNil(t, client)
		code := &model.OAuthAuthorizationCode{
			ClientID: client.ID, UserID: alice.ID, RedirectURI: "http://localhost:5555/callback",
			Scopes: []model.OAuthScope{model.ScopePageRead}, IncludePersonal: true,
			CodeChallenge: "c", CodeChallengeMethod: "S256", ExpiresAt: time.Now().Add(time.Minute),
		}
		require.NoError(t, s.codes.Create(ctx, code, "code-hash-mcp"))
		consumed, err := s.codes.ConsumeByHash(ctx, "code-hash-mcp")
		require.NoError(t, err)
		assert.True(t, consumed.IncludePersonal)
		assert.Empty(t, consumed.OrgIDs)

		refreshExp := time.Now().Add(time.Hour)
		refresh := "refresh-hash-mcp"
		tok := &model.OAuthToken{
			ClientID: client.ID, ActingUserID: alice.ID, GrantType: model.GrantAuthorizationCode,
			Scopes: consumed.Scopes, IncludePersonal: true,
			AccessTokenExpiresAt: time.Now().Add(time.Hour), RefreshTokenExpiresAt: &refreshExp,
		}
		require.NoError(t, s.tokens.Create(ctx, tok, "access-hash-mcp", &refresh))
		got, err := s.tokens.GetByAccessHash(ctx, "access-hash-mcp")
		require.NoError(t, err)
		assert.True(t, got.IncludePersonal)

		rotated, err := s.tokens.RotateRefresh(ctx, client.ID, refresh, "access-hash-2", "refresh-hash-2", time.Now().Add(time.Hour), refreshExp)
		require.NoError(t, err)
		assert.True(t, rotated.IncludePersonal)

		live, err := s.tokens.ListActiveForUser(ctx, alice.ID)
		require.NoError(t, err)
		require.Len(t, live, 1)
		assert.Equal(t, tok.ID, live[0].ID)
		assert.True(t, live[0].IncludePersonal)

		require.NoError(t, s.tokens.Revoke(ctx, tok.ID))
		live, err = s.tokens.ListActiveForUser(ctx, alice.ID)
		require.NoError(t, err)
		assert.Empty(t, live)
	})

	t.Run("SearchContentMatchesTextOnly", func(t *testing.T) {
		page, err := s.pages.Create(ctx, &model.Page{UserID: alice.ID, Type: model.NodeTypePage, Title: "Notes", Tags: []string{}})
		require.NoError(t, err)
		doc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Quarterly roadmap: 100% done"}]}]}`)
		_, err = s.pages.UpsertContent(ctx, &model.PageContent{PageID: page.ID, Content: doc, SchemaVersion: 1}, alice.ID)
		require.NoError(t, err)

		hits, err := s.pages.SearchContent(ctx, alice.ID, []uuid.UUID{page.ID}, "ROADMAP", 10)
		require.NoError(t, err)
		require.Len(t, hits, 1)
		assert.Contains(t, hits[0].Text, "Quarterly roadmap")

		// "r_admap" and "Quarterly%done" would match if _ and % weren't escaped.
		for _, q := range []string{"paragraph", "type", "r_admap", "Quarterly%done"} {
			hits, err = s.pages.SearchContent(ctx, alice.ID, []uuid.UUID{page.ID}, q, 10)
			require.NoError(t, err)
			assert.Empty(t, hits, "query %q must not match JSON structure or act as a wildcard", q)
		}
		hits, err = s.pages.SearchContent(ctx, alice.ID, []uuid.UUID{page.ID}, "100%", 10)
		require.NoError(t, err)
		assert.Len(t, hits, 1)

		// A phrase spanning differently formatted text nodes and blocks.
		marked, err := s.pages.Create(ctx, &model.Page{UserID: alice.ID, Type: model.NodeTypePage, Title: "Marked", Tags: []string{}})
		require.NoError(t, err)
		_, err = s.pages.UpsertContent(ctx, &model.PageContent{PageID: marked.ID, SchemaVersion: 1, Content: json.RawMessage(
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Written by an "},{"type":"text","marks":[{"type":"bold"}],"text":"agent"},{"type":"text","text":" today"}]},{"type":"paragraph","content":[{"type":"text","text":"second block"}]}]}`)}, alice.ID)
		require.NoError(t, err)
		for q, want := range map[string]int{"written by an AGENT": 1, "today second": 1, "agent written": 0} {
			hits, err = s.pages.SearchContent(ctx, alice.ID, []uuid.UUID{page.ID, marked.ID}, q, 10)
			require.NoError(t, err)
			assert.Len(t, hits, want, q)
		}

		// The access filter is re-applied even for ids the caller passes in.
		bob := s.createUser(t, "sub-bob-mcp", "bob-mcp@test.com", "Bob")
		hits, err = s.pages.SearchContent(ctx, bob.ID, []uuid.UUID{page.ID}, "roadmap", 10)
		require.NoError(t, err)
		assert.Empty(t, hits)
	})

	t.Run("MigrationDownThenUp", func(t *testing.T) {
		down, err := os.ReadFile("../../migrations/000018_mcp_oauth.down.sql")
		require.NoError(t, err)
		up, err := os.ReadFile("../../migrations/000018_mcp_oauth.up.sql")
		require.NoError(t, err)
		_, err = s.pool.Exec(ctx, string(down))
		require.NoError(t, err)
		var dynamic int
		require.NoError(t, s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM oauth_clients WHERE client_id = $1`, client.ClientID).Scan(&dynamic))
		assert.Zero(t, dynamic, "down migration removes creator-less dynamic clients")
		_, err = s.pool.Exec(ctx, string(up))
		require.NoError(t, err)
	})
}
