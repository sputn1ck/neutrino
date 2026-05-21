package chainimport

import (
	"fmt"
	"net/http"
	"os"
)

// httpHeaderImportSource implements headerImportSource for serving header files
// over HTTP(s).
type httpHeaderImportSource struct {
	uri          string
	httpClient   HttpClient
	file         HeaderImportSource
	tempFilePath string
}

// Compile-time assertion to ensure httpHeaderImportSource implements
// headerImportSource interface.
var _ HeaderImportSource = (*httpHeaderImportSource)(nil)

// newHTTPHeaderImportSource creates a new HTTP header import source.
func newHTTPHeaderImportSource(uri string, httpClient HttpClient,
	importSource HeaderImportSource) *httpHeaderImportSource {

	return &httpHeaderImportSource{
		uri:        uri,
		httpClient: httpClient,
		file:       importSource,
	}
}

// Open opens the HTTP header import source based on file header import source.
func (h *httpHeaderImportSource) Open() error {
	resp, err := h.httpClient.Get(h.uri)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: status code %d",
			resp.StatusCode)
	}

	return h.openBody(resp.Body)
}

// Close closes the HTTP header import source resources.
func (h *httpHeaderImportSource) Close() error {
	if err := h.file.Close(); err != nil {
		return fmt.Errorf("failed to close file import source: %w", err)
	}

	if h.tempFilePath == "" {
		return nil
	}

	if err := os.Remove(h.tempFilePath); err != nil {
		return fmt.Errorf("failed to remove temporary file: %w", err)
	}

	return nil
}

// GetHeaderMetadata reads the metadata from the file. The metadata is memoized
// after the first call, with subsequent calls returning the cached result
// without re-reading the file.
func (h *httpHeaderImportSource) GetHeaderMetadata() (*headerMetadata, error) {
	return h.file.GetHeaderMetadata()
}

// Iterator returns an efficient iterator for sequential header access.
func (h *httpHeaderImportSource) Iterator(start, end uint32,
	batchSize uint32) HeaderIterator {

	return h.file.Iterator(start, end, batchSize)
}

// GetHeader retrieves a single header at the specified index.
func (h *httpHeaderImportSource) GetHeader(index uint32) (Header, error) {
	return h.file.GetHeader(index)
}

// GetURI returns the HTTP URL for this import source.
func (h *httpHeaderImportSource) GetURI() string {
	return h.uri
}

// SetURI sets the HTTP URL for this import source.
func (h *httpHeaderImportSource) SetURI(uri string) {
	h.uri = uri
}

// newHTTPClient creates a new HTTP client.
func newHTTPClient() HttpClient {
	return &http.Client{}
}
