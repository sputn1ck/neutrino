//go:build !js || !wasm

package chainimport

import (
	"fmt"
	"io"
	"os"
)

func (h *httpHeaderImportSource) openBody(body io.Reader) error {
	tempFile, err := os.CreateTemp("", "neutrino-headers-http-import-*.tmp")
	if err != nil {
		return err
	}
	cleanup := func() {
		tempFile.Close()
		os.Remove(tempFile.Name())
	}

	_, err = io.Copy(tempFile, body)
	if err != nil {
		cleanup()
		return err
	}

	if err = tempFile.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}

	tempFile.Close()

	h.tempFilePath = tempFile.Name()
	h.file.SetURI(h.tempFilePath)
	return h.file.Open()
}
