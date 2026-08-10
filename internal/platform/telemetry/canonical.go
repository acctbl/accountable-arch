package telemetry

import (
	"fmt"

	"github.com/gowebpki/jcs"
)

func CanonicalizeJSON(raw []byte) ([]byte, error) {
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON with RFC 8785: %w", err)
	}
	return canonical, nil
}
