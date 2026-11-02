package apperr

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestAs(t *testing.T) {
	base := New(KindNotFound, "USER_NOT_FOUND", "User not found")

	tests := []struct {
		name string
		err  error
		ok   bool
	}{
		{"direct", base, true},
		{"wrapped", fmt.Errorf("wrap: %w", base), true},
		{"other", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := As(tt.err)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != base {
				t.Fatalf("got %v, want %v", got, base)
			}
		})
	}
}

func TestInvalidRequest(t *testing.T) {
	err := InvalidRequest("email", "displayName")

	if err.Kind != KindInvalid || err.Code != "INVALID_REQUEST" {
		t.Fatalf("got kind %v code %q", err.Kind, err.Code)
	}
	if !slices.Equal(err.Diagnostics.ValidationPaths, []string{"email", "displayName"}) {
		t.Fatalf("paths = %v", err.Diagnostics.ValidationPaths)
	}
}

func TestInfrastructureHidesCause(t *testing.T) {
	cause := errors.New("password authentication failed for user")
	err := Infrastructure(KindInternal, "DATABASE_ERROR", "safe", "users.insert", Diagnostics{Reason: "UnknownError"}, cause)

	if err.Error() != "safe" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if errors.Is(err, cause) {
		t.Fatal("cause must not be reachable through errors.Is")
	}
	if !errors.Is(err.Cause(), cause) {
		t.Fatal("Cause() must return the raw cause")
	}
}
