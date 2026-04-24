package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"go-tf-provisioner/internal/config"
	"go-tf-provisioner/internal/provisioner"
	"go-tf-provisioner/internal/status"
)

//go:generate go tool mockgen -destination=mocks/mock_provision_service.go -package=mocks go-tf-provisioner/internal/httpapi ProvisionService

// ProvisionService is the subset of the provisioner surface the HTTP layer
// needs. Defined here so the handlers can be tested with a generated mock.
type ProvisionService interface {
	Submit(ctx context.Context, req provisioner.ProvisionRequest) (provisioner.Job, error)
	List(ctx context.Context, customerID, productCodeFilter string) ([]status.Status, error)
}

type Server struct {
	cfg  config.Config
	prov ProvisionService
	srv  *http.Server
}

func NewServer(cfg config.Config, prov ProvisionService) *Server {
	s := &Server{cfg: cfg, prov: prov}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /provision", s.provisionHandler)
	mux.HandleFunc("GET /info", s.infoHandler)
	mux.HandleFunc("GET /healthz", s.healthHandler)
	s.srv = &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Handler exposes the router for tests.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// ListenAndServe starts the HTTP server and blocks until ctx is cancelled,
// then performs a graceful shutdown.
func (s *Server) ListenAndServe(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", s.srv.Addr)
		err := s.srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}
