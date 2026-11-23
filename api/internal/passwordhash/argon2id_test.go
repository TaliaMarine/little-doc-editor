package passwordhash

import "testing"

func TestHashAndVerify(t *testing.T) {
	hash, err := Hash("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	ok, err := Verify("correct-horse-battery-staple", hash)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("expected correct password to verify")
	}

	ok, err = Verify("wrong-password", hash)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Error("expected wrong password not to verify")
	}
}

func TestHash_ProducesDifferentSaltsEachTime(t *testing.T) {
	h1, err := Hash("same-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	h2, err := Hash("same-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 == h2 {
		t.Error("expected two hashes of the same password to differ (random salt)")
	}
}

func TestVerify_RejectsGarbageHash(t *testing.T) {
	_, err := Verify("anything", "not-a-real-hash")
	if err == nil {
		t.Error("expected an error for a malformed hash")
	}
}
