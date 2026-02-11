package client

import (
	"errors"
	"io"
	"math"
)

var ErrBodyTooLarge = errors.New("httpx/client: response body too large")

// ReadAllAndCloseLimit reads at most limit bytes from body and always closes it.
//
// If body contains more than limit bytes, it returns ErrBodyTooLarge.
// If limit <= 0, the effective limit is 0.
func ReadAllAndCloseLimit(body io.ReadCloser, limit int64) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	if limit < 0 {
		limit = 0
	}

	defer func() {
		_ = body.Close()
	}()

	// Read up to limit+1 so we can detect overflow.
	n := limit
	if limit < math.MaxInt64 {
		n = limit + 1
	}
	lr := &io.LimitedReader{R: body, N: n}
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrBodyTooLarge
	}
	return b, nil
}

// DrainAndClose drains up to max bytes from body (discarding them) and then closes it.
//
// It is useful to increase the chance of HTTP connection reuse.
//
// If max <= 0, it does not drain and only closes.
func DrainAndClose(body io.ReadCloser, max int64) error {
	if body == nil {
		return nil
	}
	var readErr error
	if max > 0 {
		_, readErr = io.Copy(io.Discard, io.LimitReader(body, max))
	}
	closeErr := body.Close()
	if readErr != nil {
		return readErr
	}
	return closeErr
}
