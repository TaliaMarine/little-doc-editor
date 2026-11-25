package sessiontoken

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "test-secret"

func TestIssueForUser_RoundTrips(t *testing.T) {
	userID := uuid.New()
	tok, err := IssueForUser(testSecret, userID, "Ada Lovelace", time.Hour)
	if err != nil {
		t.Fatalf("IssueForUser: %v", err)
	}

	claims, err := Verify(testSecret, tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("expected sub %s, got %s", userID, claims.Subject)
	}
	if claims.DisplayName != "Ada Lovelace" {
		t.Errorf("expected displayName Ada Lovelace, got %s", claims.DisplayName)
	}
	if claims.GuestIdentityID != "" {
		t.Errorf("expected no guestIdentityId for a user token, got %s", claims.GuestIdentityID)
	}
}

func TestIssueForGuest_RoundTrips(t *testing.T) {
	guestID := uuid.New()
	tok, err := IssueForGuest(testSecret, guestID, "Anonymous Otter", time.Hour)
	if err != nil {
		t.Fatalf("IssueForGuest: %v", err)
	}

	claims, err := Verify(testSecret, tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.GuestIdentityID != guestID.String() {
		t.Errorf("expected guestIdentityId %s, got %s", guestID, claims.GuestIdentityID)
	}
	if claims.Subject != "" {
		t.Errorf("expected no sub for a guest token, got %s", claims.Subject)
	}
}

func TestVerify_RejectsWrongSecret(t *testing.T) {
	tok, err := IssueForUser(testSecret, uuid.New(), "Someone", time.Hour)
	if err != nil {
		t.Fatalf("IssueForUser: %v", err)
	}
	if _, err := Verify("wrong-secret", tok); err == nil {
		t.Error("expected verification with the wrong secret to fail")
	}
}

func TestVerify_RejectsExpiredToken(t *testing.T) {
	tok, err := IssueForUser(testSecret, uuid.New(), "Someone", -time.Hour)
	if err != nil {
		t.Fatalf("IssueForUser: %v", err)
	}
	if _, err := Verify(testSecret, tok); err == nil {
		t.Error("expected verification of an expired token to fail")
	}
}

func TestVerify_RejectsGarbageToken(t *testing.T) {
	if _, err := Verify(testSecret, "not.a.jwt"); err == nil {
		t.Error("expected verification of a non-JWT string to fail")
	}
}

func TestVerifyRejectsReencodedSignature(t *testing.T) {
	tok, err := IssueForUser("secret", uuid.New(), "N", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// A 32-byte HMAC is 43 base64url characters with 2 spare bits in the last
	// one, so neighbouring characters decode to the same bytes in lax decoders.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := tok[len(tok)-1]
	idx := strings.IndexByte(alphabet, last)
	accepted := 0
	for i := 0; i < len(alphabet); i++ {
		if i == idx {
			continue
		}
		if _, err := Verify("secret", tok[:len(tok)-1]+string(alphabet[i])); err == nil {
			accepted++
		}
	}
	if accepted != 0 {
		t.Fatalf("%d non-canonical spellings of the token verified; logout revocation could be bypassed", accepted)
	}
	if _, err := Verify("secret", tok); err != nil {
		t.Fatalf("the canonical token must still verify: %v", err)
	}
}
