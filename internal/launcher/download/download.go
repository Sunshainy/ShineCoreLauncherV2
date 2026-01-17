package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Progress struct {
	BytesDownloaded int64
	BytesTotal      int64
}

type ProgressFunc func(Progress)

func EnsureFile(ctx context.Context, client *http.Client, url string, dst string, expectedSize int64, expectedSha256 string, onProgress ProgressFunc) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return EnsureFileWithRequest(ctx, client, req, dst, expectedSize, expectedSha256, onProgress)
}

func EnsureFileWithRequest(ctx context.Context, client *http.Client, req *http.Request, dst string, expectedSize int64, expectedSha256 string, onProgress ProgressFunc) error {
	attempts := 3
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cloned := req.Clone(ctx)
		cloned.Body = nil
		err := ensureFileOnce(ctx, client, cloned, dst, expectedSize, expectedSha256, onProgress)
		if err == nil {
			return nil
		}
		lastErr = err
		// retry on transient errors only
		var httpErr *httpError
		if errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
			break
		}
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	return lastErr
}

type httpError struct {
	StatusCode int
	Status     string
}

func (e *httpError) Error() string {
	return "download failed: " + e.Status
}

func ensureFileOnce(ctx context.Context, client *http.Client, req *http.Request, dst string, expectedSize int64, expectedSha256 string, onProgress ProgressFunc) error {
	if ok, _ := checkFile(dst, expectedSize, expectedSha256); ok {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	tmp := dst + ".tmp"
	_ = os.Remove(tmp)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)
	var total int64
	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := writer.Write(buf[:n]); err != nil {
				return err
			}
			total += int64(n)
			if onProgress != nil {
				onProgress(Progress{BytesDownloaded: total, BytesTotal: expectedSize})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if expectedSha256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if got != expectedSha256 {
			_ = out.Close()
			return errors.New("sha256 mismatch")
		}
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return nil
}

func checkFile(path string, expectedSize int64, expectedSha256 string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return false, nil
	}
	if expectedSha256 == "" {
		return true, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false, err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	return got == expectedSha256, nil
}
