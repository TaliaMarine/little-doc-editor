// Package sessiontoken issues and verifies the HS256 JWT that stands in for
// a session across the api and collab services. The claim shape here is
// exactly what collab/src/auth.ts expects to verify with the same shared
// secret (SPEC.md §4: "shared secret ... check" before a WS client joins a
// document room) — changing field names must change both sides together.
package sessiontoken

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("sessiontoken: invalid or expired token")

type Claims struct {
	// GuestIdentityID: the guest identity ID, for an anonymous edit-link
	// session (SPEC.md Decision 6). Exactly one of the embedded Subject
	// (sub, the user ID) and GuestIdentityID is ever set, mirroring the
	// sessions table's CHECK constraint.
	GuestIdentityID string `json:"guestIdentityId,omitempty"`
	DisplayName     string `json:"displayName"`
	// ShareLinkID is set only for a guest token issued by redeeming a share
	// link (SPEC.md §6.6). The link's page scope and permission are
	// deliberately NOT copied into the token: they're re-read from the
	// share_links row on every request (and every collab connect), so
	// revoking or expiring a link cuts off already-issued guest sessions
	// immediately rather than at token expiry. Never set for a real user's
	// token; a user's access is governed by their space role instead.
	ShareLinkID string `json:"shareLinkId,omitempty"`
	jwt.RegisteredClaims
}

func IssueForUser(secret string, userID uuid.UUID, displayName string, ttl time.Duration) (string, error) {
	claims := Claims{
		DisplayName: displayName,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(), // unique per token: logout revokes by token hash
		},
	}
	return sign(secret, claims)
}

func IssueForGuest(secret string, guestIdentityID uuid.UUID, displayName string, ttl time.Duration) (string, error) {
	claims := Claims{
		GuestIdentityID: guestIdentityID.String(),
		DisplayName:     displayName,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(), // unique per token: logout revokes by token hash
		},
	}
	return sign(secret, claims)
}

// IssueForShareGuest is IssueForGuest bound to a share link. The link's
// page (plus descendants, Decision 21) and permission are resolved from the
// share_links row at access-check time, not baked into the token.
func IssueForShareGuest(secret string, guestIdentityID, shareLinkID uuid.UUID, displayName string, ttl time.Duration) (string, error) {
	claims := Claims{
		GuestIdentityID: guestIdentityID.String(),
		DisplayName:     displayName,
		ShareLinkID:     shareLinkID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(), // unique per token: logout revokes by token hash
		},
	}
	return sign(secret, claims)
}

func sign(secret string, claims Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// Verify parses and validates token, returning its claims. Signature and
// expiry are checked; the caller is responsible for any further checks
// (e.g. that the referenced user isn't disabled).
func Verify(secret, tokenString string) (Claims, error) {
	var claims Claims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(),
		// Reject non-canonical base64url spellings of an otherwise valid
		// token: the revocation deny-list keys on a hash of the exact token
		// string, so a re-encoded variant of a logged-out token must not verify.
		jwt.WithStrictDecoding())
	if err != nil || !token.Valid {
		return Claims{}, ErrInvalidToken
	}

	hasUser := claims.Subject != ""
	hasGuest := claims.GuestIdentityID != ""
	if hasUser == hasGuest { // both set or neither set
		return Claims{}, ErrInvalidToken
	}

	return claims, nil
}
