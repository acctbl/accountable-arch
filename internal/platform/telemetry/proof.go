package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

const (
	ProofRequestBody = `{"schema_version":1,"event_name":"observability.proof"}`
	ProofType        = "end_to_end_v1"
	ProofMethod      = "POST"
	ProofPath        = "/telemetry/v1"
)

type ProofHeaders struct {
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

type ProofVerification struct {
	Headers    ProofHeaders
	Body       []byte
	ServerTime time.Time
	ActiveKey  string
	StandbyKey string
}

func ParseProofFixture(raw []byte) (ProofHeaders, error) {
	var headers ProofHeaders
	if err := decodeStrictJSON(raw, &headers); err != nil {
		return ProofHeaders{}, err
	}
	if !proofTimestampPattern.MatchString(headers.Timestamp) ||
		!releaseIDPattern.MatchString(headers.Nonce) ||
		!proofSignaturePattern.MatchString(headers.Signature) {
		return ProofHeaders{}, contractError("proof headers")
	}
	return headers, nil
}

func DecodeProofKey(encoded string) ([]byte, error) {
	if len(encoded) != 43 {
		return nil, contractError("proof key")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return nil, contractError("proof key")
	}
	return decoded, nil
}

func ProofCanonicalString(timestamp, nonce string, body []byte) string {
	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s", ProofMethod, ProofPath, timestamp, nonce, sha256Hex(body))
}

func SignProof(encodedKey, timestamp, nonce string, body []byte) (string, error) {
	key, err := DecodeProofKey(encodedKey)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(ProofCanonicalString(timestamp, nonce, body)))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func VerifyProof(input ProofVerification) error {
	if string(input.Body) != ProofRequestBody {
		return contractError("proof body")
	}
	if !proofTimestampPattern.MatchString(input.Headers.Timestamp) ||
		!releaseIDPattern.MatchString(input.Headers.Nonce) ||
		!proofSignaturePattern.MatchString(input.Headers.Signature) {
		return contractError("proof headers")
	}
	timestamp, err := strconv.ParseInt(input.Headers.Timestamp, 10, 64)
	if err != nil {
		return contractError("proof timestamp")
	}
	difference := input.ServerTime.Unix() - timestamp
	if difference < -60 || difference > 60 {
		return contractError("proof timestamp")
	}
	provided, err := hex.DecodeString(input.Headers.Signature)
	if err != nil || len(provided) != sha256.Size {
		return contractError("proof signature")
	}
	activeMatch := proofKeyMatches(input.ActiveKey, input.Headers.Timestamp, input.Headers.Nonce, input.Body, provided)
	standbyMatch := proofKeyMatches(input.StandbyKey, input.Headers.Timestamp, input.Headers.Nonce, input.Body, provided)
	if activeMatch|standbyMatch != 1 {
		return contractError("proof signature")
	}
	return nil
}

func proofKeyMatches(encodedKey, timestamp, nonce string, body, signature []byte) int {
	key, err := DecodeProofKey(encodedKey)
	if err != nil {
		key = make([]byte, sha256.Size)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(ProofCanonicalString(timestamp, nonce, body)))
	if hmac.Equal(signature, mac.Sum(nil)) && err == nil {
		return 1
	}
	return 0
}
