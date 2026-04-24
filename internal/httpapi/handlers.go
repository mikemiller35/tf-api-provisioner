package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"go-tf-provisioner/internal/provisioner"
)

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) provisionHandler(w http.ResponseWriter, r *http.Request) {
	var req provisioner.ProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	job, err := s.prov.Submit(r.Context(), req)
	if err != nil {
		var ve *provisioner.ValidationError
		switch {
		case errors.As(err, &ve):
			writeError(w, http.StatusBadRequest, ve.Msg)
		case errors.Is(err, provisioner.ErrJobInFlight):
			writeError(w, http.StatusConflict, err.Error())
		default:
			log.Printf("provision: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) infoHandler(w http.ResponseWriter, r *http.Request) {
	customerID := r.URL.Query().Get("customerId")
	productCode := r.URL.Query().Get("productCode")
	if customerID == "" {
		writeError(w, http.StatusBadRequest, "missing required query param: customerId")
		return
	}

	statuses, err := s.prov.List(r.Context(), customerID, productCode)
	if err != nil {
		log.Printf("info list: %v", err)
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
