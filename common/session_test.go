package common

import "testing"

func TestBuildSessionCookieNameUsesConfiguredValue(t *testing.T) {
	got := BuildSessionCookieName("secret-a", " custom-session ")
	if got != "custom-session" {
		t.Fatalf("expected configured cookie name, got %q", got)
	}
}

func TestBuildSessionCookieNameDerivesStableNameFromSecret(t *testing.T) {
	first := BuildSessionCookieName("secret-a", "")
	second := BuildSessionCookieName("secret-a", "")
	other := BuildSessionCookieName("secret-b", "")

	if first == "" {
		t.Fatal("expected derived cookie name to be non-empty")
	}
	if first != second {
		t.Fatalf("expected stable cookie name for same secret, got %q and %q", first, second)
	}
	if first == other {
		t.Fatalf("expected different cookie names for different secrets, got %q", first)
	}
}

func TestBuildSessionCookieNameFallsBackWhenSecretMissing(t *testing.T) {
	got := BuildSessionCookieName("", "")
	if got != DefaultSessionCookieName {
		t.Fatalf("expected fallback cookie name %q, got %q", DefaultSessionCookieName, got)
	}
}
