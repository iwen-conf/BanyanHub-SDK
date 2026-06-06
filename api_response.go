package sdk

import (
	"fmt"
	"io"
	"net/http"
)

const maxAPIResponseBodyBytes = 4 * 1024 * 1024

func readAPIJSONResponse(resp *http.Response) ([]byte, error) {
	if resp.ContentLength > maxAPIResponseBodyBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxAPIResponseBodyBytes)
	}
	raw, tooLarge, err := readLimitedBody(resp.Body, maxAPIResponseBodyBytes)
	if err != nil {
		return nil, err
	}
	if tooLarge {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxAPIResponseBodyBytes)
	}
	return raw, nil
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
