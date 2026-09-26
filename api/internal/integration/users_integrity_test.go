package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/glyph/api/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// TestUserIdentityIntegrity covers the users.Upsert / GetByEmail findings in
// the audit's Low section: a login whose IdP omits the email or name claim
// must not erase them, and an email lookup (used to share, add org members
// and resolve token subjects) must be case-insensitive and must never pick
// one of several matching accounts at random.
func TestUserIdentityIntegrity(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"UpsertKeepsEmailAndNameWhenClaimsAreMissing": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			u, err := h.UserStore.Upsert(context.Background(), "sub-alice", "test-issuer", nil, nil)
			require.NoError(t, err)
			require.Equal(t, h.UserA.ID, u.ID)
			if assert.NotNil(t, u.Email, "email was erased") {
				assert.Equal(t, "alice@test.com", *u.Email)
			}
			if assert.NotNil(t, u.Name, "name was erased") {
				assert.Equal(t, "Alice", *u.Name)
			}

			// A claim that is present still updates.
			email := "alice@new.test"
			u, err = h.UserStore.Upsert(context.Background(), "sub-alice", "test-issuer", &email, nil)
			require.NoError(t, err)
			assert.Equal(t, "alice@new.test", deref(u.Email))
			assert.Equal(t, "Alice", deref(u.Name), "name was erased")
		},

		"GetByEmailIgnoresCase": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			u, err := h.UserStore.GetByEmail(context.Background(), "ALICE@Test.com")
			require.NoError(t, err)
			assert.Equal(t, h.UserA.ID, u.ID)

			_, err = h.UserStore.GetByEmail(context.Background(), "nobody@test.com")
			assert.ErrorIs(t, err, store.ErrNotFound)
		},

		"GetByEmailRefusesAmbiguousMatch": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			// A second account (another IdP) whose address differs only in case.
			email, name := "Alice@test.com", "Other Alice"
			_, err := h.UserStore.Upsert(context.Background(), "sub-alice-2", "other-issuer", &email, &name)
			require.NoError(t, err)

			_, err = h.UserStore.GetByEmail(context.Background(), "alice@test.com")
			assert.ErrorIs(t, err, store.ErrConflict, "an ambiguous email resolved to one account")

			// Adding a member by that email is refused rather than guessed.
			orgID := createOrgAs(t, h, h.UserB.ID, "Org")
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
				map[string]string{"email": "alice@test.com", "role": "viewer"}, h.UserB.ID)
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		},
	})
}
