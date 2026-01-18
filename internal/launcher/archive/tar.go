package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ExtractTar(src, dst string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	var reader io.Reader = file
	if strings.HasSuffix(strings.ToLower(src), ".gz") || strings.HasSuffix(strings.ToLower(src), ".tgz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header == nil {
			continue
		}
		target, err := safeTarPath(dst, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return errors.New("unsupported tar entry type: " + string(header.Typeflag))
		}
	}
}

func safeTarPath(dst, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == "" {
		return "", errors.New("invalid tar entry: " + name)
	}
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", errors.New("unsafe tar entry: " + name)
	}
	return filepath.Join(dst, clean), nil
}
