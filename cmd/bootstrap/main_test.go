package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const proofKMSKeyARN = "arn:aws:kms:eu-west-2:453722413624:key/11111111-2222-3333-4444-555555555555"

func TestLoadConfigAcceptsCompleteManagedBootstrapEnvironment(t *testing.T) {
	t.Parallel()

	path := writeBootstrapConfig(t, `
schema_version = 1
revision = "bootstrap-proof"
aws_region = "eu-west-2"
cell_id = "development-01"
database_host = "database.example.com"
environment = "development"
kms_key_arn = "`+proofKMSKeyARN+`"
parameter_root = "/accountable/development-01/development"
`)
	config, err := loadConfig([]string{"--config", path})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if config.cellID != "development-01" || config.identity.Revision != "bootstrap-proof" ||
		config.identity.Fingerprint == "" ||
		config.region != "eu-west-2" || config.databaseHost != "database.example.com" ||
		config.apiSecrets.ParameterPath != "/accountable/development-01/development/api" ||
		config.migrateSecrets.ParameterPath != "/accountable/development-01/development/migrate" ||
		config.apiSecrets.KMSKeyARN != proofKMSKeyARN {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadConfigRejectsMissingOrUnsafeManagedBootstrapEnvironment(t *testing.T) {
	t.Parallel()

	base := fileConfig{
		SchemaVersion: 1, Revision: "bootstrap-proof", CellID: "development-01",
		AWSRegion: "eu-west-2", DatabaseHost: "database.example.com", Environment: "development",
		KMSKeyARN: proofKMSKeyARN, ParameterRoot: "/accountable/development-01/development",
	}
	for name, mutate := range map[string]func(*fileConfig){
		"local environment":         func(value *fileConfig) { value.Environment = "local" },
		"relative parameter root":   func(value *fileConfig) { value.ParameterRoot = "accountable/cell" },
		"traversing parameter root": func(value *fileConfig) { value.ParameterRoot = "/accountable/../cell" },
		"trailing parameter root":   func(value *fileConfig) { value.ParameterRoot = "/accountable/cell/" },
		"missing kms key":           func(value *fileConfig) { value.KMSKeyARN = "" },
		"aliased kms key":           func(value *fileConfig) { value.KMSKeyARN = "alias/aws/ssm" },
		"missing cell":              func(value *fileConfig) { value.CellID = "" },
		"missing revision":          func(value *fileConfig) { value.Revision = "" },
		"wrong schema version":      func(value *fileConfig) { value.SchemaVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := base
			mutate(&value)
			path := writeBootstrapConfig(t, `
schema_version = `+strconv.Itoa(value.SchemaVersion)+`
revision = "`+value.Revision+`"
aws_region = "`+value.AWSRegion+`"
cell_id = "`+value.CellID+`"
database_host = "`+value.DatabaseHost+`"
environment = "`+value.Environment+`"
kms_key_arn = "`+value.KMSKeyARN+`"
parameter_root = "`+value.ParameterRoot+`"
`)
			if _, err := loadConfig([]string{"--config", path}); err == nil {
				t.Fatal("loadConfig accepted an unsafe environment")
			}
		})
	}
}

func writeBootstrapConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
