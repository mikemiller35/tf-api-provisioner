package status

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"go-tf-provisioner/pkg/aws/s3"
)

var (
	ErrJobInFlight = errors.New("a job for this customer and product is already running")
	ErrNotFound    = errors.New("status not found")
)

type Store struct {
	s3     s3.Client
	bucket string
}

func NewStore(s3c s3.Client, bucket string) *Store {
	return &Store{s3: s3c, bucket: bucket}
}

// Get fetches the status object for a customer+product. Returns ErrNotFound if
// the object does not exist.
func (s *Store) Get(ctx context.Context, customerID, productCode string) (Status, error) {
	return s.getByKey(ctx, StatusKey(customerID, productCode))
}

// Put writes the status object.
func (s *Store) Put(ctx context.Context, st Status) error {
	body, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}
	key := StatusKey(st.CustomerID, st.ProductCode)
	_, err = s.s3.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("put status %s: %w", key, err)
	}
	return nil
}

// ListByCustomer returns all statuses under the customer prefix. Optionally
// filtered to a single productCode.
func (s *Store) ListByCustomer(ctx context.Context, customerID, productCodeFilter string) ([]Status, error) {
	prefix := CustomerPrefix(customerID)
	var out []Status
	var token *string
	for {
		page, err := s.s3.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("list statuses: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil || !strings.HasSuffix(*obj.Key, ".status.json") {
				continue
			}
			st, err := s.getByKey(ctx, *obj.Key)
			if err != nil {
				return nil, err
			}
			if productCodeFilter != "" && st.ProductCode != productCodeFilter {
				continue
			}
			out = append(out, st)
		}
		if page.IsTruncated == nil || !*page.IsTruncated {
			break
		}
		token = page.NextContinuationToken
	}
	return out, nil
}

// ClaimRunning atomically (best-effort) transitions the status for
// customer+product to "running". Returns ErrJobInFlight if the existing status
// is already running. The returned Status has State=running and a fresh JobID
// and StartedAt; the caller is responsible for eventually calling Put with a
// terminal state.
func (s *Store) ClaimRunning(ctx context.Context, seed Status) (Status, error) {
	existing, err := s.Get(ctx, seed.CustomerID, seed.ProductCode)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Status{}, err
	}
	if err == nil && existing.State == StateRunning {
		return Status{}, ErrJobInFlight
	}
	if err := s.Put(ctx, seed); err != nil {
		return Status{}, err
	}
	return seed, nil
}

func (s *Store) getByKey(ctx context.Context, key string) (Status, error) {
	out, err := s.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return Status{}, ErrNotFound
		}
		return Status{}, fmt.Errorf("get status %s: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()
	var st Status
	if err := json.NewDecoder(out.Body).Decode(&st); err != nil {
		return Status{}, fmt.Errorf("unmarshal status %s: %w", key, err)
	}
	return st, nil
}

func isNotFound(err error) bool {
	if _, ok := errors.AsType[*s3types.NoSuchKey](err); ok {
		return true
	}
	if _, ok := errors.AsType[*s3types.NotFound](err); ok {
		return true
	}
	if ae, ok := errors.AsType[smithy.APIError](err); ok {
		switch ae.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}
