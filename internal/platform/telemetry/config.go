package telemetry

type ConfigIdentity struct {
	SchemaVersion int    `json:"schema_version"`
	Revision      string `json:"revision"`
	Fingerprint   string `json:"fingerprint"`
}

func ParseConfigIdentity(raw []byte) (ConfigIdentity, error) {
	var identity ConfigIdentity
	if err := decodeStrictJSON(raw, &identity); err != nil {
		return ConfigIdentity{}, err
	}
	if err := ValidateConfigIdentity(identity); err != nil {
		return ConfigIdentity{}, err
	}
	return identity, nil
}

func ValidateConfigIdentity(identity ConfigIdentity) error {
	if identity.SchemaVersion != ContractSchemaVersion {
		return contractError("configuration schema_version")
	}
	if !revisionPattern.MatchString(identity.Revision) {
		return contractError("configuration revision")
	}
	if !fingerprintPattern.MatchString(identity.Fingerprint) {
		return contractError("configuration fingerprint")
	}
	return nil
}

func ConfigFingerprint(raw []byte) string {
	return sha256Hex(raw)
}
