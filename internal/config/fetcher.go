package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// FetcherValues configures diffx-fetcher. DiskLimitBytes bounds its mirrors.
type FetcherValues struct {
	ServerURL      string
	Secret         string
	Mirrors        string
	DiskLimitBytes int64
	Concurrency    int
}

// LoadFetcher reads DIFFX_FETCHER_* environment only; the fetcher has no
// config file. Diagnostics never echo the secret.
func LoadFetcher() (FetcherValues, error) {
	values := FetcherValues{ServerURL: os.Getenv("DIFFX_FETCHER_SERVER_URL"), Secret: os.Getenv("DIFFX_FETCHER_SECRET"), Mirrors: "data/mirrors", Concurrency: 2}
	if values.ServerURL == "" {
		return values, errors.New("DIFFX_FETCHER_SERVER_URL is required, such as http://server:7979")
	}
	if values.Secret == "" {
		return values, errors.New("DIFFX_FETCHER_SECRET is required; use the server's DIFFX_SERVER_FETCHER_SECRET")
	}
	if mirrors, exists := os.LookupEnv("DIFFX_FETCHER_MIRRORS"); exists {
		if mirrors == "" {
			return values, errors.New("DIFFX_FETCHER_MIRRORS must be a directory path")
		}
		values.Mirrors = mirrors
	}
	gigabytes := int64(20)
	if raw, exists := os.LookupEnv("DIFFX_FETCHER_DISK_LIMIT_GB"); exists {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 || value > 1<<20 {
			return values, fmt.Errorf("DIFFX_FETCHER_DISK_LIMIT_GB must be a positive whole number of GiB, such as 20")
		}
		gigabytes = value
	}
	values.DiskLimitBytes = gigabytes << 30
	if raw, exists := os.LookupEnv("DIFFX_FETCHER_CONCURRENCY"); exists {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 8 {
			return values, fmt.Errorf("DIFFX_FETCHER_CONCURRENCY must be between 1 and 8")
		}
		values.Concurrency = value
	}
	return values, nil
}
