package sdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxAPIResponseBodyBytes = 4 * 1024 * 1024

func decodeAPIJSONResponse(resp *http.Response, v any) error {
	if v == nil {
		return nil
	}
	if resp.ContentLength > maxAPIResponseBodyBytes {
		return fmt.Errorf("response body exceeds %d bytes", maxAPIResponseBodyBytes)
	}
	return decodeJSON(resp.Body, v)
}

// decodeJSON reads successful API responses through a hard cap before decoding.
// SDK JSON responses are expected to be small; artifacts use the OTA stream path.
func decodeJSON(r io.Reader, v any) error {
	raw, tooLarge, err := readLimitedBody(r, maxAPIResponseBodyBytes)
	if err != nil {
		return err
	}
	if tooLarge {
		return fmt.Errorf("response body exceeds %d bytes", maxAPIResponseBodyBytes)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("response body contains trailing data")
	}
	return nil
}

func readLimitedBody(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(raw)) > maxBytes {
		return raw[:maxBytes], true, nil
	}
	return raw, false, nil
}
