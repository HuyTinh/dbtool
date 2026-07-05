package postgres

import (
	"archive/tar"
	"io"
	"os"
	"time"

	"dbtool/internal/cache"
	"dbtool/internal/driver"
)

func (d *PostgresDriver) DetectFormat(filePath string) (driver.Format, error) {
	if d.cache != nil {
		fp, err := cache.ComputeFingerprint(filePath)
		if err == nil {
			var cached driver.Format
			ok, _ := d.cache.Get("format", fp.Key(), &cached)
			if ok {
				return cached, nil
			}
		}
	}

	format, err := detectFormatFromFile(filePath)
	if err != nil {
		return driver.FormatUnknown, err
	}

	if d.cache != nil {
		fp, err := cache.ComputeFingerprint(filePath)
		if err == nil {
			_ = d.cache.Set("format", fp.Key(), filePath, format, 30*24*time.Hour)
		}
	}

	return format, nil
}

func detectFormatFromFile(filePath string) (driver.Format, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return driver.FormatUnknown, err
	}

	if info.IsDir() {
		return driver.FormatDirectory, nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return driver.FormatUnknown, err
	}
	defer f.Close()

	// Read first 262 bytes for format signatures
	buf := make([]byte, 262)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return driver.FormatUnknown, err
	}

	// 1. Postgres Custom format magic bytes: PGDMP
	if n >= 5 && string(buf[:5]) == "PGDMP" {
		return driver.FormatCustom, nil
	}

	// 2. Tar check: seek back to 0 and verify if it has a tar structure
	_, _ = f.Seek(0, 0)
	tr := tar.NewReader(f)
	if _, err := tr.Next(); err == nil {
		return driver.FormatTar, nil
	}

	// 3. Fallback to Plain SQL format
	return driver.FormatPlain, nil
}
