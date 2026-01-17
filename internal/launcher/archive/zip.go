package archive

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ExtractZip(src, dst string) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if err := extractFile(file, dst); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(file *zip.File, dst string) error {
	if strings.Contains(file.Name, "..") {
		return errors.New("unsafe zip entry: " + file.Name)
	}
	target := filepath.Join(dst, filepath.FromSlash(file.Name))
	if file.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := file.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
