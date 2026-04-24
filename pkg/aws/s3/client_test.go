package s3_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	s3 "go-tf-provisioner/pkg/aws/s3"
)

func TestS3(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pkg/aws/s3 Suite")
}

var _ = Describe("NewClient", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		// Pin region so LoadDefaultConfig is deterministic regardless of host env.
		GinkgoT().Setenv("AWS_REGION", "us-east-1")
	})

	It("returns a default client when AWS_ENDPOINT_URL is unset", func() {
		GinkgoT().Setenv("AWS_ENDPOINT_URL", "")

		c, err := s3.NewClient(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(c).NotTo(BeNil())

		s3c, ok := c.(*awss3.Client)
		Expect(ok).To(BeTrue(), "expected concrete *awss3.Client")
		opts := s3c.Options()
		Expect(opts.BaseEndpoint).To(BeNil())
		Expect(opts.UsePathStyle).To(BeFalse())
	})

	It("overrides the endpoint and enables path-style when AWS_ENDPOINT_URL is set", func() {
		const endpoint = "http://localhost:4566"
		GinkgoT().Setenv("AWS_ENDPOINT_URL", endpoint)

		c, err := s3.NewClient(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(c).NotTo(BeNil())

		s3c, ok := c.(*awss3.Client)
		Expect(ok).To(BeTrue(), "expected concrete *awss3.Client")
		opts := s3c.Options()
		Expect(aws.ToString(opts.BaseEndpoint)).To(Equal(endpoint))
		Expect(opts.UsePathStyle).To(BeTrue())
	})
})
