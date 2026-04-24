package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

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
	cfg    config.Config
	prov   ProvisionService
	logger *zap.Logger
	srv    *http.Server
}

func NewServer(cfg config.Config, prov ProvisionService, logger *zap.Logger) *Server {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &Server{cfg: cfg, prov: prov, logger: logger}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(logger))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.healthHandler)
	r.Get("/info", s.infoHandler)

	r.Group(func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"))
		r.Post("/provision", s.provisionHandler)
	})

	s.srv = &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           r,
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
		s.logger.Info("listening", zap.String("addr", s.srv.Addr))
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
