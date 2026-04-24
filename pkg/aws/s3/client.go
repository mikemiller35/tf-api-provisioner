// Package s3 provides a minimal S3 client surface and a constructor that
// respects AWS_ENDPOINT_URL for LocalStack/MinIO testing.
package s3

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

//go:generate go tool mockgen -destination=mocks/mock_s3_client.go -package=mocks go-tf-provisioner/pkg/aws/s3 Client

// Client is the subset of the AWS S3 API this project depends on.
type Client interface {
	GetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	HeadObject(ctx context.Context, params *awss3.HeadObjectInput, optFns ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *awss3.ListObjectsV2Input, optFns ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error)
}

// NewClient builds an S3 client from the default AWS config. If
// AWS_ENDPOINT_URL is set, it's used as the base endpoint with path-style
// addressing enabled (for LocalStack/MinIO).
func NewClient(ctx context.Context) (Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	if endpoint := os.Getenv("AWS_ENDPOINT_URL"); endpoint != "" {
		return awss3.NewFromConfig(cfg, func(o *awss3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}), nil
	}

	return awss3.NewFromConfig(cfg), nil
}
