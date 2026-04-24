package modules_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"go-tf-provisioner/internal/modules"
	"go-tf-provisioner/pkg/aws/s3/mocks"
)

func zipBytes(entries map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, contents := range entries {
		w, err := zw.Create(name)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Write([]byte(contents))
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(zw.Close()).To(Succeed())
	return buf.Bytes()
}

func etagOf(body []byte) string {
	sum := md5.Sum(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

var _ = Describe("modules.Fetcher", func() {
	var (
		ctrl    *gomock.Controller
		mockS3  *mocks.MockClient
		cacheDir string
		fetcher *modules.Fetcher
		ctx     context.Context
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3 = mocks.NewMockClient(ctrl)
		cacheDir = GinkgoT().TempDir()
		fetcher = modules.NewFetcher(mockS3, "modules", cacheDir)
		ctx = context.Background()
	})

	AfterEach(func() { ctrl.Finish() })

	Describe("Fetch", func() {
		It("downloads, extracts, and returns a usable module directory", func() {
			body := zipBytes(map[string]string{
				"main.tf":            "resource \"null_resource\" \"x\" {}\n",
				"modules/sub/sub.tf": "variable \"y\" {}\n",
			})
			etag := etagOf(body)

			mockS3.EXPECT().HeadObject(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
					Expect(aws.ToString(in.Bucket)).To(Equal("modules"))
					Expect(aws.ToString(in.Key)).To(Equal("widget.zip"))
					return &s3.HeadObjectOutput{ETag: aws.String(etag)}, nil
				})
			mockS3.EXPECT().GetObject(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
					Expect(aws.ToString(in.Key)).To(Equal("widget.zip"))
					return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body))}, nil
				})

			path, err := fetcher.Fetch(ctx, "widget")
			Expect(err).NotTo(HaveOccurred())
			Expect(filepath.Join(path, "main.tf")).To(BeAnExistingFile())
			Expect(filepath.Join(path, "modules", "sub", "sub.tf")).To(BeAnExistingFile())
		})

		It("hits the on-disk cache when the ETag is unchanged", func() {
			body := zipBytes(map[string]string{"main.tf": "x"})
			etag := etagOf(body)

			// First call: one Head, one Get.
			mockS3.EXPECT().HeadObject(gomock.Any(), gomock.Any()).
				Return(&s3.HeadObjectOutput{ETag: aws.String(etag)}, nil).Times(2)
			mockS3.EXPECT().GetObject(gomock.Any(), gomock.Any()).
				Return(&s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body))}, nil).Times(1)

			first, err := fetcher.Fetch(ctx, "widget")
			Expect(err).NotTo(HaveOccurred())
			second, err := fetcher.Fetch(ctx, "widget")
			Expect(err).NotTo(HaveOccurred())
			Expect(second).To(Equal(first))
		})

		It("re-downloads when the ETag changes", func() {
			body1 := zipBytes(map[string]string{"main.tf": "v1"})
			body2 := zipBytes(map[string]string{"main.tf": "v2"})
			etag1, etag2 := etagOf(body1), etagOf(body2)

			gomock.InOrder(
				mockS3.EXPECT().HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{ETag: aws.String(etag1)}, nil),
				mockS3.EXPECT().GetObject(gomock.Any(), gomock.Any()).
					Return(&s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body1))}, nil),
				mockS3.EXPECT().HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{ETag: aws.String(etag2)}, nil),
				mockS3.EXPECT().GetObject(gomock.Any(), gomock.Any()).
					Return(&s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body2))}, nil),
			)

			p1, err := fetcher.Fetch(ctx, "widget")
			Expect(err).NotTo(HaveOccurred())
			p2, err := fetcher.Fetch(ctx, "widget")
			Expect(err).NotTo(HaveOccurred())
			Expect(p2).NotTo(Equal(p1))

			got, err := os.ReadFile(filepath.Join(p2, "main.tf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal("v2"))
		})
	})

	Describe("CopyTree", func() {
		It("copies files recursively preserving directory structure", func() {
			src := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(src, "sub"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0o644)).To(Succeed())

			dst := filepath.Join(GinkgoT().TempDir(), "out")
			Expect(modules.CopyTree(src, dst)).To(Succeed())
			Expect(filepath.Join(dst, "a.txt")).To(BeAnExistingFile())
			Expect(filepath.Join(dst, "sub", "b.txt")).To(BeAnExistingFile())
		})
	})
})
