package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/kukalajet/go-backend-template/internal/apperr"
	"github.com/kukalajet/go-backend-template/internal/domain"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// Timestamp serializes as UTC with milliseconds, the format of JavaScript's
// Date.toISOString().
type Timestamp struct{ time.Time }

// MarshalJSON implements json.Marshaler.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.UTC().Format("2006-01-02T15:04:05.000Z") + `"`), nil
}

type userResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	CreatedAt   Timestamp `json:"createdAt"`
}

func toUserResponse(user domain.User) userResponse {
	return userResponse{
		ID:          user.ID.String(),
		Email:       user.Email.String(),
		DisplayName: user.DisplayName.String(),
		CreatedAt:   Timestamp{user.CreatedAt},
	}
}

type listUsersResponse struct {
	Users []userResponse `json:"users"`
}

type createUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		return err
	}

	users, err := s.users.List(r.Context(), limit)
	if err != nil {
		return err
	}

	body := listUsersResponse{Users: make([]userResponse, 0, len(users))}
	for _, user := range users {
		body.Users = append(body.Users, toUserResponse(user))
	}

	return writeJSON(w, http.StatusOK, body)
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultListLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxListLimit {
		return 0, apperr.InvalidRequest("limit")
	}

	return limit, nil
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) error {
	id, err := domain.ParseUserID(r.PathValue("id"))
	if err != nil {
		return err
	}

	user, err := s.users.FindByID(r.Context(), id)
	if err != nil {
		return err
	}

	return writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}

	in, err := domain.NewCreateUser(req.Email, req.DisplayName)
	if err != nil {
		return err
	}

	user, err := s.users.Insert(r.Context(), in)
	if err != nil {
		return err
	}

	w.Header().Set("Location", "/users/"+user.ID.String())

	return writeJSON(w, http.StatusCreated, toUserResponse(user))
}
