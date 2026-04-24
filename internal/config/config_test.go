package config_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go-tf-provisioner/internal/config"
)

var _ = Describe("Config.Load", func() {
	BeforeEach(func() {
		for _, k := range []string{
			"AWS_REGION", "TF_MODULE_BUCKET", "TF_STATUS_BUCKET", "TF_STATE_BUCKET", "TF_STATE_DYNAMODB_TABLE",
			"TF_STATE_REGION", "PORT", "TF_BINARY_PATH", "TF_WORK_DIR",
			"TF_PLUGIN_CACHE_DIR", "TF_JOB_TIMEOUT",
		} {
			GinkgoT().Setenv(k, "")
		}
	})

	When("required env vars are missing", func() {
		It("returns an error listing every missing var", func() {
			_, err := config.Load()
			Expect(err).To(HaveOccurred())
			for _, want := range []string{"AWS_REGION", "TF_MODULE_BUCKET", "TF_STATUS_BUCKET", "TF_STATE_BUCKET", "TF_STATE_DYNAMODB_TABLE"} {
				Expect(err.Error()).To(ContainSubstring(want))
			}
		})
	})

	When("all required env vars are set", func() {
		BeforeEach(func() {
			GinkgoT().Setenv("AWS_REGION", "us-east-1")
			GinkgoT().Setenv("TF_MODULE_BUCKET", "mods")
			GinkgoT().Setenv("TF_STATUS_BUCKET", "statuses")
			GinkgoT().Setenv("TF_STATE_BUCKET", "state")
			GinkgoT().Setenv("TF_STATE_DYNAMODB_TABLE", "locks")
		})

		It("applies defaults for optional settings", func() {
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Port).To(Equal(8080))
			Expect(cfg.StateRegion).To(Equal("us-east-1"))
			Expect(cfg.TerraformBinary).To(Equal("/usr/local/bin/terraform"))
			Expect(cfg.WorkDir).To(Equal("/var/lib/tf-provisioner"))
			Expect(cfg.PluginCacheDir).To(Equal("/var/lib/tf-provisioner/plugin-cache"))
			Expect(cfg.JobTimeout).To(Equal(30 * time.Minute))
		})

		It("honors TF_JOB_TIMEOUT overrides", func() {
			GinkgoT().Setenv("TF_JOB_TIMEOUT", "5m")
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.JobTimeout).To(Equal(5 * time.Minute))
		})

		It("honors TF_STATE_REGION overrides", func() {
			GinkgoT().Setenv("TF_STATE_REGION", "eu-west-1")
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.StateRegion).To(Equal("eu-west-1"))
		})
	})
})
