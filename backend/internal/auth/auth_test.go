package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("demo1234")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "demo1234") {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("expected wrong password to fail verification")
	}
}

func TestGenerateAndParseToken(t *testing.T) {
	token, err := GenerateToken(42, "secret")
	if err != nil {
		t.Fatal(err)
	}
	userID, err := ParseToken(token, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if userID != 42 {
		t.Fatalf("expected userID 42, got %d", userID)
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	token, _ := GenerateToken(42, "secret")
	if _, err := ParseToken(token, "other-secret"); err == nil {
		t.Fatal("expected error for token signed with a different secret")
	}
}
