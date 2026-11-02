package httpapi

import (
	"net/http"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

// statusClientClosedRequest records in the access log that the client left.
const statusClientClosedRequest = 499

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Fields  []string `json:"fields,omitempty"`
}

func status(k apperr.Kind) int {
	switch k {
	case apperr.KindInvalid:
		return http.StatusBadRequest
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindUnavailable:
		return http.StatusServiceUnavailable
	case apperr.KindTooLarge:
		return http.StatusRequestEntityTooLarge
	case apperr.KindInternal:
		return http.StatusInternalServerError
	}

	return http.StatusInternalServerError
}

type handler func(w http.ResponseWriter, r *http.Request) error

func (s *Server) handle(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		w.WriteHeader(statusClientClosedRequest)

		return
	}

	appErr, ok := apperr.As(err)
	if !ok {
		s.logger(r).Error("Unexpected error", "error", err)
		writeErrorBody(w, http.StatusInternalServerError, internalError())

		return
	}

	if appErr.Kind == apperr.KindInternal || appErr.Kind == apperr.KindUnavailable {
		s.logger(r).Error("Operation failed",
			"op", appErr.Op, "code", appErr.Code, "diagnostics", appErr.Diagnostics)
	}
	writeErrorBody(w, status(appErr.Kind), errorResponse{Error: errorDetail{
		Code:    appErr.Code,
		Message: appErr.Message,
		Fields:  clientFields(appErr),
	}})
}

// clientFields exposes field paths only for client mistakes, so the paths of a
// stored row that fails validation stay in the logs.
func clientFields(appErr *apperr.Error) []string {
	if appErr.Kind != apperr.KindInvalid || appErr.Diagnostics == nil {
		return nil
	}

	return appErr.Diagnostics.ValidationPaths
}

func internalError() errorResponse {
	return errorResponse{Error: errorDetail{Code: "INTERNAL_ERROR", Message: "An unexpected error occurred"}}
}

func writeErrorBody(w http.ResponseWriter, code int, body errorResponse) {
	_ = writeJSON(w, code, body) // errorResponse always encodes
}
