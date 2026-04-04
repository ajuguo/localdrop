package localdrop

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	defaultAddr           = "0.0.0.0:8499"
	defaultDataDir        = "data"
	defaultDatabaseName   = "localdrop.db"
	defaultImagesDirName  = "images"
	defaultMaxUploadBytes = 20 << 20
)

type Config struct {
	Addr           string
	DataDir        string
	DBPath         string
	ImagesDir      string
	WebDevURL      string
	MaxUploadBytes int64
}

func LoadConfig() (Config, error) {
	addr := getenv("LOCALDROP_ADDR", defaultAddr)
	dataDir := getenv("LOCALDROP_DATA_DIR", defaultDataDir)
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir: %w", err)
	}

	maxUploadBytes := int64(defaultMaxUploadBytes)
	if raw := os.Getenv("LOCALDROP_MAX_UPLOAD_MB"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("invalid LOCALDROP_MAX_UPLOAD_MB: %q", raw)
		}
		maxUploadBytes = int64(value) << 20
	}

	return Config{
		Addr:           addr,
		DataDir:        absDataDir,
		DBPath:         filepath.Join(absDataDir, defaultDatabaseName),
		ImagesDir:      filepath.Join(absDataDir, defaultImagesDirName),
		WebDevURL:      os.Getenv("LOCALDROP_WEB_DEV_URL"),
		MaxUploadBytes: maxUploadBytes,
	}, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
