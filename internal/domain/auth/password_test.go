package auth

import "testing"

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("VerifyPassword matching: %v", err)
	}
	if err := VerifyPassword(hash, "wrong password"); err == nil {
		t.Fatalf("VerifyPassword non-matching: expected error, got nil")
	}
}

func TestHashPasswordProducesDifferentSalts(t *testing.T) {
	a, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected different encoded hashes for same password (different salts)")
	}
}

func TestVerifyPasswordRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-phc-string",
		"$argon2id$v=19$m=65536,t=3,p=2$badsalt$badhash",
	}
	for _, c := range cases {
		if err := VerifyPassword(c, "whatever"); err == nil {
			t.Errorf("expected error for input %q", c)
		}
	}
}
