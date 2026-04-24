package cmd

import (
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"go-tf-provisioner/internal/awsclient"
	"go-tf-provisioner/internal/config"
	"go-tf-provisioner/internal/httpapi"
	"go-tf-provisioner/internal/modules"
	"go-tf-provisioner/internal/provisioner"
	"go-tf-provisioner/internal/status"
	"go-tf-provisioner/internal/tfrunner"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the Terraform provisioning HTTP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		s3c, err := awsclient.NewS3Client(ctx)
		if err != nil {
			return err
		}

		store := status.NewStore(s3c, cfg.StateBucket)
		fetcher := modules.NewFetcher(s3c, cfg.ModuleBucket, cfg.WorkDir+"/cache")
		runner := tfrunner.New(tfrunner.Config{
			Binary:         cfg.TerraformBinary,
			StateBucket:    cfg.StateBucket,
			StateRegion:    cfg.StateRegion,
			StateLockTable: cfg.StateLockTable,
			PluginCacheDir: cfg.PluginCacheDir,
		})
		prov := provisioner.New(cfg, store, fetcher, runner)

		return httpapi.NewServer(cfg, prov).ListenAndServe(ctx)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
