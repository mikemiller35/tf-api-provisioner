package provisioner

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"go-tf-provisioner/internal/config"
	"go-tf-provisioner/internal/modules"
	"go-tf-provisioner/internal/status"
	"go-tf-provisioner/internal/tfrunner"
)

type ProvisionRequest struct {
	CustomerID   string `json:"customerId"`
	ProductCode  string `json:"productCode"`
	CompanyName  string `json:"companyName"`
	ContactEmail string `json:"contactEmail"`
}

// ValidationError wraps field-level input problems. Handlers map this to 400.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func (r ProvisionRequest) Validate() error {
	var missing []string
	if strings.TrimSpace(r.CustomerID) == "" {
		missing = append(missing, "customerId")
	}
	if strings.TrimSpace(r.ProductCode) == "" {
		missing = append(missing, "productCode")
	}
	if strings.TrimSpace(r.CompanyName) == "" {
		missing = append(missing, "companyName")
	}
	if strings.TrimSpace(r.ContactEmail) == "" {
		missing = append(missing, "contactEmail")
	}
	if len(missing) > 0 {
		return &ValidationError{Msg: "missing required fields: " + strings.Join(missing, ", ")}
	}
	if _, err := mail.ParseAddress(r.ContactEmail); err != nil {
		return &ValidationError{Msg: "invalid contactEmail: " + err.Error()}
	}
	return nil
}

type Job struct {
	JobID      string `json:"jobId"`
	StatusKey  string `json:"statusKey"`
	CustomerID string `json:"customerId"`
}

type Provisioner struct {
	cfg     config.Config
	store   *status.Store
	fetcher *modules.Fetcher
	runner  *tfrunner.Runner
}

func New(cfg config.Config, store *status.Store, fetcher *modules.Fetcher, runner *tfrunner.Runner) *Provisioner {
	return &Provisioner{cfg: cfg, store: store, fetcher: fetcher, runner: runner}
}

// Submit validates the request, claims a running status slot, and kicks off a
// background goroutine that runs terraform. Returns status.ErrJobInFlight if a
// job is already running for the same customer+product.
func (p *Provisioner) Submit(ctx context.Context, req ProvisionRequest) (Job, error) {
	if err := req.Validate(); err != nil {
		return Job{}, err
	}

	now := time.Now().UTC()
	seed := status.Status{
		JobID:        uuid.NewString(),
		CustomerID:   req.CustomerID,
		ProductCode:  req.ProductCode,
		CompanyName:  req.CompanyName,
		ContactEmail: req.ContactEmail,
		State:        status.StateRunning,
		StateKey:     status.StateKey(req.CustomerID, req.ProductCode),
		StartedAt:    now,
	}

	claimed, err := p.store.ClaimRunning(ctx, seed)
	if err != nil {
		return Job{}, err
	}

	go p.run(claimed, req)

	return Job{
		JobID:      claimed.JobID,
		StatusKey:  status.StatusKey(req.CustomerID, req.ProductCode),
		CustomerID: req.CustomerID,
	}, nil
}

// List returns all statuses for a customer, optionally filtered by productCode.
func (p *Provisioner) List(ctx context.Context, customerID, productCodeFilter string) ([]status.Status, error) {
	return p.store.ListByCustomer(ctx, customerID, productCodeFilter)
}

func (p *Provisioner) run(st status.Status, req ProvisionRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.JobTimeout)
	defer cancel()

	logBuf := &bytes.Buffer{}
	logger := log.New(os.Stdout, fmt.Sprintf("[job=%s customer=%s product=%s] ", st.JobID, st.CustomerID, st.ProductCode), log.LstdFlags)
	logger.Printf("starting terraform apply")

	runDir := filepath.Join(p.cfg.WorkDir, "runs", st.JobID)
	defer func() {
		if err := os.RemoveAll(runDir); err != nil {
			logger.Printf("cleanup runDir failed: %v", err)
		}
	}()

	result, runErr := p.runJob(ctx, st, req, runDir, logBuf)

	endedAt := time.Now().UTC()
	final := st
	final.EndedAt = &endedAt

	if runErr != nil {
		final.State = status.StateFailed
		final.Error = formatErr(runErr, logBuf.String())
		logger.Printf("terraform apply failed: %v", runErr)
	} else {
		final.State = status.StateSucceeded
		final.Outputs = result.Outputs
		logger.Printf("terraform apply succeeded")
	}

	// Persist final status with a fresh context; the job ctx may have been
	// cancelled (timeout, shutdown) but we still want the failure recorded.
	putCtx, putCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer putCancel()
	if err := p.store.Put(putCtx, final); err != nil {
		logger.Printf("failed to persist final status: %v", err)
	}
}

func (p *Provisioner) runJob(ctx context.Context, st status.Status, req ProvisionRequest, runDir string, logSink *bytes.Buffer) (tfrunner.RunResult, error) {
	modulePath, err := p.fetcher.Fetch(ctx, req.ProductCode)
	if err != nil {
		return tfrunner.RunResult{}, fmt.Errorf("fetch module: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(runDir), 0o755); err != nil {
		return tfrunner.RunResult{}, err
	}
	if err := modules.CopyTree(modulePath, runDir); err != nil {
		return tfrunner.RunResult{}, fmt.Errorf("materialize run dir: %w", err)
	}

	return p.runner.Apply(ctx, tfrunner.RunInput{
		RunDir:       runDir,
		CustomerID:   st.CustomerID,
		ProductCode:  st.ProductCode,
		CompanyName:  st.CompanyName,
		ContactEmail: st.ContactEmail,
		LogSink:      logSink,
	})
}

func formatErr(err error, logs string) string {
	msg := err.Error()
	logs = strings.TrimSpace(logs)
	if logs == "" {
		return msg
	}
	// Cap the embedded log excerpt so status objects don't grow unbounded.
	const max = 4000
	if len(logs) > max {
		logs = "..." + logs[len(logs)-max:]
	}
	return msg + "\n---\n" + logs
}

// ErrJobInFlight is re-exported so handlers can match on it without depending
// on the status package directly.
var ErrJobInFlight = status.ErrJobInFlight
