// Package domain holds the business types and their invariants.
package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

const (
	maxEmailLength       = 320
	maxDisplayNameLength = 100
)

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// UserID identifies a user.
type UserID struct{ value uuid.UUID }

// ParseUserID accepts only the canonical 36-character form, so each user has
// one valid ID string.
func ParseUserID(s string) (UserID, error) {
	id, err := uuid.Parse(s)
	if err != nil || id.String() != s {
		return UserID{}, apperr.InvalidRequest("id")
	}

	return UserID{value: id}, nil
}

// UserIDFromUUID wraps an ID that is already a valid UUID.
func UserIDFromUUID(id uuid.UUID) UserID { return UserID{value: id} }

// UUID returns the underlying UUID.
func (id UserID) UUID() uuid.UUID { return id.value }

// String returns the canonical form.
func (id UserID) String() string { return id.value.String() }

// Email is a trimmed, lowercased email address.
type Email struct{ value string }

// ParseEmail normalizes and validates an email address.
func ParseEmail(s string) (Email, error) {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if utf8.RuneCountInString(normalized) > maxEmailLength || !emailPattern.MatchString(normalized) {
		return Email{}, apperr.InvalidRequest("email")
	}

	return Email{value: normalized}, nil
}

// String returns the normalized address.
func (e Email) String() string { return e.value }

// DisplayName is a trimmed, non-empty name.
type DisplayName struct{ value string }

// ParseDisplayName normalizes and validates a display name.
func ParseDisplayName(s string) (DisplayName, error) {
	normalized := strings.TrimSpace(s)
	length := utf8.RuneCountInString(normalized)
	if length < 1 || length > maxDisplayNameLength {
		return DisplayName{}, apperr.InvalidRequest("displayName")
	}

	return DisplayName{value: normalized}, nil
}

// String returns the normalized name.
func (d DisplayName) String() string { return d.value }

// User is a stored user.
type User struct {
	ID          UserID
	Email       Email
	DisplayName DisplayName
	CreatedAt   time.Time
}

// CreateUser is a validated request to create a user.
type CreateUser struct {
	Email       Email
	DisplayName DisplayName
}

// NewCreateUser validates the input and reports every failing field.
func NewCreateUser(email, displayName string) (CreateUser, error) {
	parsedEmail, parsedName, err := parseUserFields(email, displayName)
	if err != nil {
		return CreateUser{}, err
	}

	return CreateUser{Email: parsedEmail, DisplayName: parsedName}, nil
}

// RestoreUser rebuilds a user from stored values, which may no longer be valid.
func RestoreUser(id UserID, email, displayName string, createdAt time.Time) (User, error) {
	parsedEmail, parsedName, err := parseUserFields(email, displayName)
	if err != nil {
		return User{}, err
	}

	return User{ID: id, Email: parsedEmail, DisplayName: parsedName, CreatedAt: createdAt}, nil
}

func parseUserFields(email, displayName string) (Email, DisplayName, error) {
	var paths []string
	parsedEmail, err := ParseEmail(email)
	if err != nil {
		paths = append(paths, "email")
	}
	parsedName, err := ParseDisplayName(displayName)
	if err != nil {
		paths = append(paths, "displayName")
	}
	if len(paths) > 0 {
		return Email{}, DisplayName{}, apperr.InvalidRequest(paths...)
	}

	return parsedEmail, parsedName, nil
}

// UserNotFound reports a missing user.
func UserNotFound() *apperr.Error {
	return apperr.New(apperr.KindNotFound, "USER_NOT_FOUND", "User not found")
}

// UserAlreadyExists reports a duplicate email.
func UserAlreadyExists() *apperr.Error {
	return apperr.New(apperr.KindConflict, "USER_ALREADY_EXISTS",
		"A user with this email already exists")
}
