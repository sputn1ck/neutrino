//go:build js && wasm

package chainimport

import (
	"bytes"
	"fmt"
	"io"
)

func (h *httpHeaderImportSource) openBody(body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	source, ok := h.file.(*fileHeaderImportSource)
	if !ok {
		return fmt.Errorf("wasm HTTP import requires file header source")
	}

	return source.openImportHeadersFile(&memoryImportHeadersFile{
		Reader: bytes.NewReader(data),
	})
}

type memoryImportHeadersFile struct {
	*bytes.Reader
}

var _ ImportHeadersFile = (*memoryImportHeadersFile)(nil)

func (m *memoryImportHeadersFile) Close() error {
	return nil
}
