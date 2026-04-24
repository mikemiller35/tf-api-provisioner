package modules

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"go-tf-provisioner/pkg/aws/s3"
)

type Fetcher struct {
	s3       s3.Client
	bucket   string
	cacheDir string
}

func NewFetcher(s3c s3.Client, bucket, cacheDir string) *Fetcher {
	return &Fetcher{s3: s3c, bucket: bucket, cacheDir: cacheDir}
}

// Fetch downloads and extracts the module zip for productCode, returning the
// path to a directory containing main.tf (and any subdirs like modules/).
// Results are cached by ETag under {cacheDir}/{productCode}/{etag}/.
func (f *Fetcher) Fetch(ctx context.Context, productCode string) (string, error) {
	etag, err := f.headETag(ctx, productCode)
	if err != nil {
		return "", err
	}

	dest := filepath.Join(f.cacheDir, productCode, etag)
	if isDir(dest) {
		return dest, nil
	}
	return f.populateCache(ctx, productCode, dest)
}

// headETag issues a HEAD on the module object and returns a path-safe ETag.
func (f *Fetcher) headETag(ctx context.Context, productCode string) (string, error) {
	key := productCode + ".zip"
	head, err := f.s3.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(f.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("head module %s: %w", key, err)
	}
	etag := sanitizeETag(aws.ToString(head.ETag))
	if etag == "" {
		return "", errors.New("module object has no ETag")
	}
	return etag, nil
}

// populateCache downloads and extracts the module zip into a sibling temp dir,
// then atomically renames it into dest. If a concurrent caller wins the
// rename, their extracted copy is used.
func (f *Fetcher) populateCache(ctx context.Context, productCode, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".extract-*")
	if err != nil {
		return "", fmt.Errorf("mkdir tmp: %w", err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmp)
		}
	}()

	zipPath := filepath.Join(tmp, "module.zip")
	if err := f.downloadZip(ctx, productCode, zipPath); err != nil {
		return "", err
	}
	if err := extractZip(zipPath, tmp); err != nil {
		return "", fmt.Errorf("extract module zip: %w", err)
	}
	if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
		return "", err
	}

	if err := os.Rename(tmp, dest); err != nil {
		if isDir(dest) {
			return dest, nil
		}
		return "", fmt.Errorf("finalize module cache: %w", err)
	}
	cleanupTmp = false
	return dest, nil
}

// downloadZip streams the module zip object to dstPath.
func (f *Fetcher) downloadZip(ctx context.Context, productCode, dstPath string) error {
	key := productCode + ".zip"
	obj, err := f.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(f.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("get module %s: %w", key, err)
	}
	defer func() { _ = obj.Body.Close() }()

	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, obj.Body); err != nil {
		_ = out.Close()
		return fmt.Errorf("write module zip: %w", err)
	}
	return out.Close()
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// extractZip unpacks zipPath into destDir. Zip-slip is prevented by rooting all
// writes at destDir via *os.Root, which refuses paths that escape.
func extractZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	for _, zf := range zr.File {
		name := filepath.Clean(zf.Name)
		if name == "." {
			continue
		}

		if zf.FileInfo().IsDir() {
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}

		if dir := filepath.Dir(name); dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}

		if err := copyZipEntry(root, zf, name); err != nil {
			return err
		}
	}
	return nil
}

func copyZipEntry(root *os.Root, zf *zip.File, name string) error {
	in, err := zf.Open()
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// CopyTree copies src into dst recursively. dst must not exist.
func CopyTree(src, dst string) error {
	return os.CopyFS(dst, os.DirFS(src))
}

// sanitizeETag strips surrounding quotes and any characters unsafe for a
// filesystem path.
func sanitizeETag(etag string) string {
	etag = strings.Trim(etag, `"`)
	etag = strings.ReplaceAll(etag, "/", "_")
	etag = strings.ReplaceAll(etag, ":", "_")
	return etag
}
