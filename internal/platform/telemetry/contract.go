package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"
)

var (
	uuidV7Pattern           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	digestPattern           = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	fingerprintPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	revisionPattern         = regexp.MustCompile(`^[-A-Za-z0-9._]{1,128}$`)
	cellIDPattern           = regexp.MustCompile(`^[a-z][a-z0-9-]{2,15}$`)
	commitSHAPattern        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	proofTimestampPattern   = regexp.MustCompile(`^[1-9][0-9]{9,10}$`)
	proofSignaturePattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	boundedNamePattern      = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	immutableURIPattern     = regexp.MustCompile(`^(oci|s3)://[^[:space:]]+$`)
	boundedAlarmNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)
	millisecondUTCPattern   = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{1,9}\+0000$`)
	registeredEnvironments  = []string{"development", "staging", "production"}
)

func sha256Hex(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validWholeSecondUTC(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Nanosecond() == 0 && parsed.Format(time.RFC3339) == value
}
