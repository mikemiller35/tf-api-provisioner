package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"go.uber.org/zap"

	"go-tf-provisioner/internal/provisioner"
)

const maxRequestBodyBytes = 1 << 20 // 1 MiB

func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) provisionHandler(w http.ResponseWriter, r *http.Request) {
	logger := loggerFrom(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req provisioner.ProvisionRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	job, err := s.prov.Submit(r.Context(), req)
	if err != nil {
		code, msg := submitErrorResponse(err)
		if code == http.StatusInternalServerError {
			logger.Error("provision submit failed", zap.Error(err))
		}
		writeError(w, code, msg)
		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

// submitErrorResponse maps provisioner.Submit errors to an HTTP status and
// user-facing message. Unknown errors become 500 with a generic message so
// internals don't leak through the response body.
func submitErrorResponse(err error) (int, string) {
	if ve, ok := errors.AsType[*provisioner.ValidationError](err); ok {
		return http.StatusBadRequest, ve.Msg
	}
	if errors.Is(err, provisioner.ErrJobInFlight) {
		return http.StatusConflict, err.Error()
	}
	return http.StatusInternalServerError, "internal error"
}

func (s *Server) infoHandler(w http.ResponseWriter, r *http.Request) {
	logger := loggerFrom(r.Context())

	customerID := r.URL.Query().Get("customerId")
	productCode := r.URL.Query().Get("productCode")
	if customerID == "" {
		writeError(w, http.StatusBadRequest, "missing required query param: customerId")
		return
	}

	statuses, err := s.prov.List(r.Context(), customerID, productCode)
	if err != nil {
		logger.Error("info list failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(statuses) == 0 {
		writeError(w, http.StatusNotFound, "no statuses found for customerId")
		return
	}
	writeJSON(w, http.StatusOK, statuses)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
