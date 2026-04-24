package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "go-tf-provisioner",
	Short: "HTTP service that runs Terraform modules per customer+product",
}

// Execute runs the root command. Called by main.main().
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
