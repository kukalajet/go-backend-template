package httpapi

import (
	"net/http"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) error {
	return writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// ready overrides the generic error mapping: any failure means not ready.
func (s *Server) ready(w http.ResponseWriter, r *http.Request) error {
	if err := s.health.Health(r.Context()); err != nil {
		log := s.logger(r)
		if appErr, ok := apperr.As(err); ok {
			log.Error("Database health check failed",
				"op", appErr.Op, "code", appErr.Code, "diagnostics", appErr.Diagnostics)
		} else {
			log.Error("Database health check failed", "error", err)
		}
		writeErrorBody(w, http.StatusServiceUnavailable, errorResponse{Error: errorDetail{
			Code:    "DATABASE_UNAVAILABLE",
			Message: "The database is temporarily unavailable",
		}})

		return nil
	}

	return writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Database: "ok"})
}
