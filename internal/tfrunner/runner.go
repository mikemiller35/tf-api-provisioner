package tfrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"

	"go-tf-provisioner/internal/status"
)

type Config struct {
	Binary         string
	StateBucket    string
	StateRegion    string
	StateLockTable string
	PluginCacheDir string
}

type Runner struct {
	cfg Config
}

func New(cfg Config) *Runner {
	return &Runner{cfg: cfg}
}

// RunInput is everything the runner needs to execute a terraform apply for
// one customer+product invocation.
type RunInput struct {
	RunDir       string
	CustomerID   string
	ProductCode  string
	CompanyName  string
	ContactEmail string
	LogSink      io.Writer
}

type RunResult struct {
	Outputs map[string]string
}

// Apply writes tfvars.json into runDir, runs init with dynamic backend config,
// applies, and returns captured outputs as string values.
func (r *Runner) Apply(ctx context.Context, in RunInput) (RunResult, error) {
	if err := writeTFVars(in); err != nil {
		return RunResult{}, err
	}

	tf, err := tfexec.NewTerraform(in.RunDir, r.cfg.Binary)
	if err != nil {
		return RunResult{}, fmt.Errorf("new terraform: %w", err)
	}
	if in.LogSink != nil {
		tf.SetStdout(in.LogSink)
		tf.SetStderr(in.LogSink)
	}
	if r.cfg.PluginCacheDir != "" {
		if err := os.MkdirAll(r.cfg.PluginCacheDir, 0o755); err != nil {
			return RunResult{}, fmt.Errorf("mkdir plugin cache: %w", err)
		}
		_ = tf.SetEnv(map[string]string{
			"TF_PLUGIN_CACHE_DIR": r.cfg.PluginCacheDir,
		})
	}

	initOpts := []tfexec.InitOption{
		tfexec.BackendConfig("bucket=" + r.cfg.StateBucket),
		tfexec.BackendConfig("key=" + status.StateKey(in.CustomerID, in.ProductCode)),
		tfexec.BackendConfig("region=" + r.cfg.StateRegion),
		tfexec.BackendConfig("dynamodb_table=" + r.cfg.StateLockTable),
		tfexec.Reconfigure(true),
	}
	if err := tf.Init(ctx, initOpts...); err != nil {
		return RunResult{}, fmt.Errorf("terraform init: %w", err)
	}

	if err := tf.Apply(ctx); err != nil {
		return RunResult{}, fmt.Errorf("terraform apply: %w", err)
	}

	outs, err := tf.Output(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("terraform output: %w", err)
	}

	result := RunResult{Outputs: map[string]string{}}
	for k, v := range outs {
		// OutputMeta.Value is json.RawMessage; keep it as a JSON-encoded string so
		// structured values survive round-tripping into the status object.
		result.Outputs[k] = string(v.Value)
	}
	return result, nil
}

func writeTFVars(in RunInput) error {
	vars := map[string]string{
		"customer_id":   in.CustomerID,
		"product_code":  in.ProductCode,
		"company_name":  in.CompanyName,
		"contact_email": in.ContactEmail,
	}
	body, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(in.RunDir, "terraform.tfvars.json"), body, 0o644)
}
