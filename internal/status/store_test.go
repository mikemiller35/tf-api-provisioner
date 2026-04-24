package status_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"go-tf-provisioner/internal/status"
	"go-tf-provisioner/pkg/aws/s3/mocks"
)

// backingStore is a tiny in-memory bucket used by gomock DoAndReturn stubs, so
// the specs exercise real store logic against consistent S3 responses without
// pinning each Get/Put call order.
type backingStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newBacking() *backingStore { return &backingStore{data: map[string][]byte{}} }

func (b *backingStore) put(key string, body []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = append([]byte(nil), body...)
}

func (b *backingStore) get(key string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.data[key]
	return v, ok
}

func (b *backingStore) keys() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.data))
	for k := range b.data {
		out = append(out, k)
	}
	return out
}

func wireMockAsBacking(m *mocks.MockClient, store *backingStore) {
	m.EXPECT().GetObject(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			body, ok := store.get(aws.ToString(in.Key))
			if !ok {
				return nil, &s3types.NoSuchKey{}
			}
			sum := md5.Sum(body)
			return &s3.GetObjectOutput{
				Body: io.NopCloser(bytes.NewReader(body)),
				ETag: aws.String(`"` + hex.EncodeToString(sum[:]) + `"`),
			}, nil
		}).AnyTimes()

	m.EXPECT().PutObject(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			body, err := io.ReadAll(in.Body)
			if err != nil {
				return nil, err
			}
			store.put(aws.ToString(in.Key), body)
			return &s3.PutObjectOutput{}, nil
		}).AnyTimes()

	m.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			prefix := aws.ToString(in.Prefix)
			var contents []s3types.Object
			for _, k := range store.keys() {
				if prefix != "" && !strings.HasPrefix(k, prefix) {
					continue
				}
				key := k
				contents = append(contents, s3types.Object{Key: aws.String(key)})
			}
			isTrunc := false
			return &s3.ListObjectsV2Output{Contents: contents, IsTruncated: &isTrunc}, nil
		}).AnyTimes()
}

func seed(customerID, productCode string, state status.State) status.Status {
	return status.Status{
		JobID:        "job-1",
		CustomerID:   customerID,
		ProductCode:  productCode,
		CompanyName:  "Acme",
		ContactEmail: "a@b.c",
		State:        state,
		StateKey:     status.StateKey(customerID, productCode),
		StartedAt:    time.Now().UTC(),
	}
}

var _ = Describe("status.Store", func() {
	var (
		ctrl    *gomock.Controller
		mockS3  *mocks.MockClient
		backing *backingStore
		store   *status.Store
		ctx     context.Context
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3 = mocks.NewMockClient(ctrl)
		backing = newBacking()
		wireMockAsBacking(mockS3, backing)
		store = status.NewStore(mockS3, "state")
		ctx = context.Background()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("Put then Get", func() {
		It("round-trips a status object", func() {
			want := seed("c1", "widget", status.StateSucceeded)
			Expect(store.Put(ctx, want)).To(Succeed())

			got, err := store.Get(ctx, "c1", "widget")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.JobID).To(Equal(want.JobID))
			Expect(got.State).To(Equal(status.StateSucceeded))
			Expect(got.CompanyName).To(Equal("Acme"))
		})
	})

	Describe("Get on missing object", func() {
		It("returns ErrNotFound", func() {
			_, err := store.Get(ctx, "nope", "widget")
			Expect(errors.Is(err, status.ErrNotFound)).To(BeTrue())
		})
	})

	Describe("ClaimRunning", func() {
		When("no prior status exists", func() {
			It("writes the running status", func() {
				_, err := store.ClaimRunning(ctx, seed("c1", "widget", status.StateRunning))
				Expect(err).NotTo(HaveOccurred())

				got, err := store.Get(ctx, "c1", "widget")
				Expect(err).NotTo(HaveOccurred())
				Expect(got.State).To(Equal(status.StateRunning))
			})
		})

		When("an existing job is running", func() {
			It("returns ErrJobInFlight without overwriting", func() {
				first := seed("c1", "widget", status.StateRunning)
				_, err := store.ClaimRunning(ctx, first)
				Expect(err).NotTo(HaveOccurred())

				second := seed("c1", "widget", status.StateRunning)
				second.JobID = "job-2"
				_, err = store.ClaimRunning(ctx, second)
				Expect(errors.Is(err, status.ErrJobInFlight)).To(BeTrue())

				got, err := store.Get(ctx, "c1", "widget")
				Expect(err).NotTo(HaveOccurred())
				Expect(got.JobID).To(Equal("job-1"))
			})
		})

		When("a prior job has reached a terminal state", func() {
			It("accepts a new running claim", func() {
				Expect(store.Put(ctx, seed("c1", "widget", status.StateSucceeded))).To(Succeed())

				next := seed("c1", "widget", status.StateRunning)
				next.JobID = "job-2"
				_, err := store.ClaimRunning(ctx, next)
				Expect(err).NotTo(HaveOccurred())

				got, err := store.Get(ctx, "c1", "widget")
				Expect(err).NotTo(HaveOccurred())
				Expect(got.JobID).To(Equal("job-2"))
			})
		})
	})

	Describe("ListByCustomer", func() {
		BeforeEach(func() {
			Expect(store.Put(ctx, seed("c1", "widget", status.StateSucceeded))).To(Succeed())
			Expect(store.Put(ctx, seed("c1", "gadget", status.StateFailed))).To(Succeed())
			Expect(store.Put(ctx, seed("c2", "widget", status.StateSucceeded))).To(Succeed())
		})

		It("returns every status for the customer", func() {
			got, err := store.ListByCustomer(ctx, "c1", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(2))
		})

		It("filters by productCode when provided", func() {
			got, err := store.ListByCustomer(ctx, "c1", "widget")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].ProductCode).To(Equal("widget"))
		})

		It("returns empty list for unknown customer", func() {
			got, err := store.ListByCustomer(ctx, "missing", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})
	})

	Describe("Key helpers", func() {
		It("formats state and status keys", func() {
			Expect(status.StateKey("c1", "widget")).To(Equal("customers/c1/widget.tfstate"))
			Expect(status.StatusKey("c1", "widget")).To(Equal("customers/c1/widget.status.json"))
			Expect(status.CustomerPrefix("c1")).To(Equal("customers/c1/"))
		})
	})
})
