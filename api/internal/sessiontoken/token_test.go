package sessiontoken

import (
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
