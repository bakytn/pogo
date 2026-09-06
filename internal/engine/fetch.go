package engine

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultDataURL is the production pvpoke game data endpoint.
const DefaultDataURL = "https://pvpoke.com/data/gamemaster.json"

// FetchGameData downloads game data from url into path, creating parent
// directories as needed. It returns the number of bytes written.
func FetchGameData(url, path string) (int64, error) {
	if url == "" {
		url = DefaultDataURL
	}
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "pogo-bot/0.1 (+pvpoke game data)")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return 0, fmt.Errorf("write: %w", err)
	}
	return n, nil
}

// DiskSize returns the byte size of path, or -1 if it does not exist.
func DiskSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}
