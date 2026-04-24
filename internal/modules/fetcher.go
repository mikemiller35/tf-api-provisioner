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
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"go-tf-provisioner/internal/awsclient"
)

type Fetcher struct {
	s3       awsclient.S3Client
	bucket   string
	cacheDir string
}

func NewFetcher(s3c awsclient.S3Client, bucket, cacheDir string) *Fetcher {
	return &Fetcher{s3: s3c, bucket: bucket, cacheDir: cacheDir}
}

// Fetch downloads and extracts the module zip for productCode, returning the
// path to a directory containing main.tf (and any subdirs like modules/).
// Results are cached by ETag under {cacheDir}/{productCode}/{etag}/.
func (f *Fetcher) Fetch(ctx context.Context, productCode string) (string, error) {
	key := productCode + ".zip"
	head, err := f.s3.HeadObject(ctx, &s3.HeadObjectInput{
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

	dest := filepath.Join(f.cacheDir, productCode, etag)
	if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
		return dest, nil
	}

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

	obj, err := f.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(f.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("get module %s: %w", key, err)
	}
	defer func() { _ = obj.Body.Close() }()

	zipPath := filepath.Join(tmp, "module.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(zipFile, obj.Body); err != nil {
		_ = zipFile.Close()
		return "", fmt.Errorf("write module zip: %w", err)
	}
	if err := zipFile.Close(); err != nil {
		return "", err
	}

	if err := extractZip(zipPath, tmp); err != nil {
		return "", fmt.Errorf("extract module zip: %w", err)
	}
	if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
		return "", err
	}

	if err := os.Rename(tmp, dest); err != nil {
		// If another request extracted concurrently and won the rename, take theirs.
		if _, serr := os.Stat(dest); serr == nil {
			return dest, nil
		}
		return "", fmt.Errorf("finalize module cache: %w", err)
	}
	cleanupTmp = false
	return dest, nil
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}

	for _, zf := range r.File {
		target := filepath.Join(absDest, zf.Name)
		// Zip-slip protection: ensure the extracted path stays inside destDir.
		rel, err := filepath.Rel(absDest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("zip entry %q escapes destination", zf.Name)
		}

		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		in, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
		if err != nil {
			_ = in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = in.Close()
			_ = out.Close()
			return err
		}
		_ = in.Close()
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

// CopyTree copies src into dst recursively. dst must not exist.
func CopyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}

// sanitizeETag strips surrounding quotes and any characters unsafe for a
// filesystem path.
func sanitizeETag(etag string) string {
	etag = strings.Trim(etag, `"`)
	etag = strings.ReplaceAll(etag, "/", "_")
	etag = strings.ReplaceAll(etag, ":", "_")
	return etag
}
