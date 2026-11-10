package selfupdate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// download fetches url, failing for a non-200 answer or more than limit bytes.
func (u Updater) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := readLimited(resp.Body, limit)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	return data, nil
}

// readLimited reads r to the end, failing if it holds more than limit bytes.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("larger than %d bytes", limit)
	}
	return data, err
}

// checksumFor returns the SHA-256 listed for file in a checksums.txt.
func checksumFor(sums []byte, file string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == file {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s (is this platform released?)", file)
}
