package auth

import (
	"encoding/base64"
	"fmt"
)

// decodeB64URL decodes state/nonce parameters.
func decodeB64URL(s string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("bad encoding")
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("bad length")
	}
	return raw, nil
}
