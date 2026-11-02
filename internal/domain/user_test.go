package domain

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

func paths(t *testing.T, err error) []string {
	t.Helper()
	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("got %v, want *apperr.Error", err)
	}
	if appErr.Kind != apperr.KindInvalid {
		t.Fatalf("kind = %v, want KindInvalid", appErr.Kind)
	}

	return appErr.Diagnostics.ValidationPaths
}

func TestParseUserID(t *testing.T) {
	const canonical = "0192f3a4-7b1c-7def-8a12-3456789abcde"

	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"canonical", canonical, true},
		{"uppercase", strings.ToUpper(canonical), false},
		{"urn", "urn:uuid:" + canonical, false},
		{"braced", "{" + canonical + "}", false},
		{"raw hex", strings.ReplaceAll(canonical, "-", ""), false},
		{"garbage", "not-a-uuid", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseUserID(tt.input)
			if !tt.ok {
				if got := paths(t, err); !slices.Equal(got, []string{"id"}) {
					t.Fatalf("paths = %v", got)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.String() != canonical {
				t.Fatalf("String() = %q", id.String())
			}
		})
	}
}

func TestParseEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"valid", "a@x.com", "a@x.com", true},
		{"normalized", "  A@X.Com ", "a@x.com", true},
		{"no at", "ax.com", "", false},
		{"no dot", "a@x", "", false},
		{"two ats", "a@b@x.com", "", false},
		{"inner space", "a b@x.com", "", false},
		{"empty", "", "", false},
		{"max runes", strings.Repeat("é", 314) + "@x.com", strings.Repeat("é", 314) + "@x.com", true},
		{"too long", strings.Repeat("é", 315) + "@x.com", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEmail(tt.input)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v, want ok = %v", err, tt.ok)
			}
			if got.String() != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"valid", "Ada", "Ada", true},
		{"trimmed", "  Ada ", "Ada", true},
		{"blank", "   ", "", false},
		{"max runes", strings.Repeat("é", 100), strings.Repeat("é", 100), true},
		{"too long", strings.Repeat("é", 101), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDisplayName(tt.input)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v, want ok = %v", err, tt.ok)
			}
			if got.String() != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The paths double as API field names, because httpapi returns them to clients.
func TestNewCreateUserReportsEveryField(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		displayName string
		want        []string
	}{
		{"both", "bad", " ", []string{"email", "displayName"}},
		{"email", "bad", "Ada", []string{"email"}},
		{"display name", "a@x.com", " ", []string{"displayName"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCreateUser(tt.email, tt.displayName)
			if got := paths(t, err); !slices.Equal(got, tt.want) {
				t.Fatalf("paths = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRestoreUser(t *testing.T) {
	id := UserIDFromUUID(uuid.New())
	now := time.Now()

	user, err := RestoreUser(id, "a@x.com", "Ada", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != id || user.Email.String() != "a@x.com" || !user.CreatedAt.Equal(now) {
		t.Fatalf("got %+v", user)
	}

	_, err = RestoreUser(id, "bad", "Ada", now)
	if got := paths(t, err); !slices.Equal(got, []string{"email"}) {
		t.Fatalf("paths = %v", got)
	}
}
