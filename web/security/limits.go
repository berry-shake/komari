package security

import (
	"errors"
	"io"
)

const (
	MaxMessageBytes int64 = 1 << 20
	MaxThemeBytes   int64 = 64 << 20
	MaxBackupBytes  int64 = 256 << 20
)

var ErrBodyTooLarge = errors.New("request body exceeds size limit")

// ReadBounded also limits decoded data, where Content-Length cannot help.
func ReadBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrBodyTooLarge
	}
	return data, nil
}
