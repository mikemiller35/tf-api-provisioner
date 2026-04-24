package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go-tf-provisioner/internal/config"
	"go-tf-provisioner/internal/httpapi"
	"go-tf-provisioner/internal/modules"
	"go-tf-provisioner/internal/provisioner"
	"go-tf-provisioner/internal/status"
	"go-tf-provisioner/internal/tfrunner"
	"go-tf-provisioner/pkg/aws/s3"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the Terraform provisioning HTTP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		logger, err := newLogger(os.Getenv("APP_ENV"), os.Getenv("LOG_LEVEL"))
		if err != nil {
			return fmt.Errorf("build logger: %w", err)
		}
		defer func() { _ = logger.Sync() }()

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		s3c, err := s3.NewClient(ctx)
		if err != nil {
			return err
		}

		store := status.NewStore(s3c, cfg.StatusBucket)
		fetcher := modules.NewFetcher(s3c, cfg.ModuleBucket, cfg.WorkDir+"/cache")
		runner := tfrunner.New(tfrunner.Config{
			Binary:         cfg.TerraformBinary,
			StateBucket:    cfg.StateBucket,
			StateRegion:    cfg.StateRegion,
			StateLockTable: cfg.StateLockTable,
			PluginCacheDir: cfg.PluginCacheDir,
		})
		prov := provisioner.New(cfg, store, fetcher, runner, logger)

		return httpapi.NewServer(cfg, prov, logger).ListenAndServe(ctx)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// newLogger builds a zap.Logger based on APP_ENV and LOG_LEVEL env vars.
// Defaults to production JSON at info level; APP_ENV=dev flips to a
// human-readable console encoder.
func newLogger(appEnv, logLevel string) (*zap.Logger, error) {
	var cfg zap.Config
	if strings.EqualFold(appEnv, "dev") || strings.EqualFold(appEnv, "development") {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	if logLevel != "" {
		var lvl zapcore.Level
		if err := lvl.UnmarshalText([]byte(logLevel)); err != nil {
			return nil, fmt.Errorf("invalid LOG_LEVEL %q: %w", logLevel, err)
		}
		cfg.Level = zap.NewAtomicLevelAt(lvl)
	}
	return cfg.Build()
}
