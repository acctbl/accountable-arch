package bootstrap

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/acctbl/accountable/internal/platform/crypto"
	"github.com/acctbl/accountable/internal/platform/database"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/storage"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

const (
	TimeProviderSystem           = "system"
	TimeProviderLinux            = "linux"
	RuntimeRoleAPI               = "api"
	RuntimeRoleMigrate           = "migrate"
	RuntimeRolePreflight         = "preflight"
	DeploymentModeLocal          = "local"
	DeploymentModeManaged        = "managed"
	AccessPurposeFoundationProof = "foundation-proof"
)

var (
	postgresIdentifier           = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	awsRegionPattern             = regexp.MustCompile(`^[a-z]{2}(?:-gov)?-[a-z]+-\d$`)
	awsAccountPattern            = regexp.MustCompile(`^[0-9]{12}$`)
	s3BucketPattern              = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	awsKMSKeyARNPattern          = regexp.MustCompile(`^arn:aws(?:-us-gov)?:kms:([a-z0-9-]+):([0-9]{12}):key/[A-Za-z0-9-]+$`)
	configurationRevisionPattern = regexp.MustCompile(`^[-A-Za-z0-9._]{1,128}$`)
	cellIDPattern                = regexp.MustCompile(`^[-A-Za-z0-9._]{1,64}$`)
	parameterPathPattern         = regexp.MustCompile(`^(?:/[A-Za-z0-9._-]+){1,14}$`)
)

type Config struct {
	SchemaVersion          int
	Revision               string
	Environment            string
	DeploymentMode         string
	CellID                 string
	AWSRegion              string
	RuntimeRole            string
	Fingerprint            string
	Capabilities           Capabilities
	Telemetry              telemetry.RuntimeConfig
	CheckTimeout           time.Duration
	ReadinessProbeInterval time.Duration
	Secrets                secret.Config
	Database               database.Config
	Storage                storage.Config
	Crypto                 crypto.Config
	Time                   TimeConfig
}

type Capabilities struct {
	ArchitectureProbe bool
	Postgres          bool
	Secrets           bool
	KMS               bool
	ObjectStorage     bool
	Telemetry         bool
}

type TimeConfig struct {
	Provider        string
	MaxClockError   time.Duration
	MaxDatabaseSkew time.Duration
}

type FileConfig struct {
	SchemaVersion          int                    `toml:"schema_version"`
	Revision               string                 `toml:"revision"`
	Environment            string                 `toml:"environment"`
	DeploymentMode         string                 `toml:"deployment_mode"`
	CellID                 string                 `toml:"cell_id"`
	AWSRegion              string                 `toml:"aws_region"`
	RuntimeRole            string                 `toml:"runtime_role"`
	Capabilities           CapabilitiesFileConfig `toml:"capabilities"`
	Secrets                *SecretsFileConfig     `toml:"secrets"`
	Postgres               *DatabaseFileConfig    `toml:"postgres"`
	ObjectStorage          *StorageFileConfig     `toml:"object_storage"`
	KMS                    *CryptoFileConfig      `toml:"kms"`
	Telemetry              *TelemetryFileConfig   `toml:"telemetry"`
	Time                   *TimeFileConfig        `toml:"time"`
	CheckTimeout           string                 `toml:"foundation_check_timeout"`
	ReadinessProbeInterval string                 `toml:"readiness_probe_interval"`
}

type CapabilitiesFileConfig struct {
	ArchitectureProbe *bool `toml:"architecture_probe"`
	Postgres          *bool `toml:"postgres"`
	Secrets           *bool `toml:"secrets"`
	KMS               *bool `toml:"kms"`
	ObjectStorage     *bool `toml:"object_storage"`
	Telemetry         *bool `toml:"telemetry"`
	Redpanda          *bool `toml:"redpanda"`
}

type SecretsFileConfig struct {
	Provider      string `toml:"provider"`
	Directory     string `toml:"directory"`
	AWSRegion     string `toml:"aws_region"`
	ParameterPath string `toml:"parameter_path"`
}

type DatabaseFileConfig struct {
	Host                string `toml:"host"`
	Port                uint16 `toml:"port"`
	Name                string `toml:"name"`
	User                string `toml:"user"`
	Role                string `toml:"role"`
	PasswordRef         string `toml:"password_ref"`
	TLSMode             string `toml:"tls_mode"`
	TLSRootCAFile       string `toml:"tls_root_ca_file"`
	ConnectTimeout      string `toml:"connect_timeout"`
	StatementTimeout    string `toml:"statement_timeout"`
	HealthCheckInterval string `toml:"health_check_interval"`
	MaxConnections      int32  `toml:"max_connections"`
	Timezone            string `toml:"timezone"`
}

type StorageFileConfig struct {
	Provider      string `toml:"provider"`
	Root          string `toml:"root"`
	Region        string `toml:"region"`
	Bucket        string `toml:"bucket"`
	Prefix        string `toml:"prefix"`
	ExpectedOwner string `toml:"expected_owner"`
	KMSKeyARN     string `toml:"kms_key_arn"`
	AccessPurpose string `toml:"access_purpose"`
}

type CryptoFileConfig struct {
	Provider                string `toml:"provider"`
	KeyRef                  string `toml:"key_ref"`
	Region                  string `toml:"region"`
	KMSKeyARN               string `toml:"kms_key_arn"`
	EncryptionContextPrefix string `toml:"encryption_context_prefix"`
}

type TelemetryFileConfig struct {
	CollectorEndpoint      string `toml:"collector_endpoint"`
	Protocol               string `toml:"protocol"`
	SamplingPolicy         string `toml:"sampling_policy"`
	RedactionPolicyVersion string `toml:"redaction_policy_version"`
}

type TimeFileConfig struct {
	Provider        string `toml:"provider"`
	MaxClockError   string `toml:"max_clock_error"`
	MaxDatabaseSkew string `toml:"max_database_skew"`
}

func Parse(configPath, fingerprint string, raw FileConfig) (Config, error) {
	if raw.SchemaVersion != 1 {
		return Config{}, errors.New("schema_version must be 1")
	}
	if !configurationRevisionPattern.MatchString(raw.Revision) {
		return Config{}, errors.New("revision must be 1 to 128 characters using only letters, digits, '.', '_', or '-'")
	}
	if raw.Environment != "development" && raw.Environment != "staging" && raw.Environment != "production" {
		return Config{}, errors.New("environment must be development, staging, or production")
	}
	if raw.DeploymentMode != DeploymentModeLocal && raw.DeploymentMode != DeploymentModeManaged {
		return Config{}, errors.New("deployment_mode must be local or managed")
	}
	managed := raw.DeploymentMode == DeploymentModeManaged
	if raw.Environment != "development" && !managed {
		return Config{}, errors.New("staging and production deployment_mode must be managed")
	}
	if !cellIDPattern.MatchString(raw.CellID) {
		return Config{}, errors.New("cell_id must be 1 to 64 opaque identifier characters")
	}
	if !awsRegionPattern.MatchString(raw.AWSRegion) {
		return Config{}, errors.New("aws_region is invalid")
	}
	if raw.RuntimeRole != RuntimeRoleAPI && raw.RuntimeRole != RuntimeRoleMigrate && raw.RuntimeRole != RuntimeRolePreflight {
		return Config{}, errors.New("runtime_role must be api, migrate, or preflight")
	}
	capabilities, err := parseCapabilities(raw)
	if err != nil {
		return Config{}, err
	}
	if raw.Environment == "production" && capabilities.ArchitectureProbe {
		return Config{}, errors.New("production preflight: capabilities.architecture_probe must be false")
	}
	if managed && !capabilities.Telemetry {
		return Config{}, errors.New("a managed runtime must enable capabilities.telemetry")
	}
	if capabilities.Postgres && !capabilities.Secrets {
		return Config{}, errors.New("capabilities.postgres requires capabilities.secrets")
	}
	if !managed && capabilities.KMS && !capabilities.Secrets {
		return Config{}, errors.New("local capabilities.kms requires capabilities.secrets")
	}
	checkTimeout, err := parsePositiveDuration("foundation_check_timeout", raw.CheckTimeout)
	if err != nil {
		return Config{}, err
	}
	readinessProbeInterval, err := parsePositiveDuration("readiness_probe_interval", raw.ReadinessProbeInterval)
	if err != nil {
		return Config{}, err
	}
	var secretsConfig secret.Config
	if capabilities.Secrets {
		secretsConfig, err = parseSecrets(managed, raw.AWSRegion, configPath, *raw.Secrets)
		if err != nil {
			return Config{}, err
		}
	}
	var databaseConfig database.Config
	if capabilities.Postgres {
		databaseConfig, err = parseDatabase(managed, configPath, *raw.Postgres)
		if err != nil {
			return Config{}, err
		}
	}
	var storageConfig storage.Config
	if capabilities.ObjectStorage {
		storageConfig, err = parseStorage(managed, raw.AWSRegion, configPath, *raw.ObjectStorage)
		if err != nil {
			return Config{}, err
		}
	}
	var telemetryConfig telemetry.RuntimeConfig
	if capabilities.Telemetry {
		telemetryConfig = telemetry.RuntimeConfig{
			CollectorEndpoint:      raw.Telemetry.CollectorEndpoint,
			Protocol:               raw.Telemetry.Protocol,
			SamplingPolicy:         raw.Telemetry.SamplingPolicy,
			RedactionPolicyVersion: raw.Telemetry.RedactionPolicyVersion,
		}
		if err := telemetry.ValidateRuntimeConfig(telemetryConfig); err != nil {
			return Config{}, err
		}
	}
	var cryptoConfig crypto.Config
	if capabilities.KMS {
		cryptoConfig, err = parseCrypto(managed, raw.AWSRegion, *raw.KMS)
		if err != nil {
			return Config{}, err
		}
	}
	timeConfig, err := parseTime(managed, *raw.Time)
	if err != nil {
		return Config{}, err
	}
	return Config{
		SchemaVersion: raw.SchemaVersion, Revision: raw.Revision, Environment: raw.Environment,
		DeploymentMode: raw.DeploymentMode, CellID: raw.CellID, AWSRegion: raw.AWSRegion,
		RuntimeRole: raw.RuntimeRole, Fingerprint: fingerprint,
		Capabilities: capabilities, Telemetry: telemetryConfig,
		CheckTimeout: checkTimeout, ReadinessProbeInterval: readinessProbeInterval,
		Secrets: secretsConfig, Database: databaseConfig, Storage: storageConfig, Crypto: cryptoConfig, Time: timeConfig,
	}, nil
}

func parseCapabilities(raw FileConfig) (Capabilities, error) {
	checks := []struct {
		name    string
		enabled *bool
		section bool
	}{
		{"postgres", raw.Capabilities.Postgres, raw.Postgres != nil},
		{"secrets", raw.Capabilities.Secrets, raw.Secrets != nil},
		{"kms", raw.Capabilities.KMS, raw.KMS != nil},
		{"object_storage", raw.Capabilities.ObjectStorage, raw.ObjectStorage != nil},
		{"telemetry", raw.Capabilities.Telemetry, raw.Telemetry != nil},
	}
	if raw.Capabilities.ArchitectureProbe == nil || raw.Capabilities.Telemetry == nil || raw.Capabilities.Redpanda == nil {
		return Capabilities{}, errors.New("every capability must be explicitly true or false")
	}
	for _, check := range checks {
		if check.enabled == nil {
			return Capabilities{}, fmt.Errorf("capabilities.%s must be explicitly true or false", check.name)
		}
		if *check.enabled != check.section {
			return Capabilities{}, fmt.Errorf("capabilities.%s contradicts the %s section", check.name, check.name)
		}
	}
	if *raw.Capabilities.Redpanda {
		return Capabilities{}, errors.New("capabilities.redpanda must be false until Redpanda is implemented")
	}
	if raw.Time == nil {
		return Capabilities{}, errors.New("time section is required")
	}
	return Capabilities{
		ArchitectureProbe: *raw.Capabilities.ArchitectureProbe,
		Postgres:          *raw.Capabilities.Postgres, Secrets: *raw.Capabilities.Secrets,
		KMS: *raw.Capabilities.KMS, ObjectStorage: *raw.Capabilities.ObjectStorage,
		Telemetry: *raw.Capabilities.Telemetry,
	}, nil
}

func parseSecrets(managed bool, awsRegion, configPath string, raw SecretsFileConfig) (secret.Config, error) {
	if !managed {
		if raw.Provider != secret.ProviderFile || raw.Directory == "" {
			return secret.Config{}, errors.New("local secrets.provider must be file with a directory")
		}
		if raw.AWSRegion != "" || raw.ParameterPath != "" {
			return secret.Config{}, errors.New("file secrets cannot include Parameter Store settings")
		}
		return secret.Config{Provider: raw.Provider, Directory: absoluteFrom(filepath.Dir(configPath), raw.Directory)}, nil
	}
	if raw.Directory != "" {
		return secret.Config{}, errors.New("managed secrets cannot include a file directory")
	}
	if raw.Provider != secret.ProviderParameterStore {
		return secret.Config{}, errors.New("managed secrets.provider must be aws_parameter_store")
	}
	if !awsRegionPattern.MatchString(raw.AWSRegion) {
		return secret.Config{}, errors.New("secrets aws_region is invalid")
	}
	if raw.AWSRegion != awsRegion {
		return secret.Config{}, errors.New("secrets aws_region must match the top-level aws_region")
	}
	if err := validateParameterPath(raw.ParameterPath); err != nil {
		return secret.Config{}, err
	}
	return secret.Config{
		Provider:      raw.Provider,
		AWSRegion:     raw.AWSRegion,
		ParameterPath: raw.ParameterPath,
	}, nil
}

func validateParameterPath(value string) error {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		path.Clean(value) != value || !parameterPathPattern.MatchString(value) {
		return errors.New("secrets parameter_path must be an absolute clean Parameter Store path")
	}
	return nil
}

func parseDatabase(managed bool, configPath string, raw DatabaseFileConfig) (database.Config, error) {
	if raw.Host == "" || raw.Port == 0 || raw.Name == "" {
		return database.Config{}, errors.New("database host, port, and name are required")
	}
	if !postgresIdentifier.MatchString(raw.User) || !postgresIdentifier.MatchString(raw.Role) {
		return database.Config{}, errors.New("database user and role must be PostgreSQL identifiers")
	}
	passwordRef, err := secret.ParseRef(raw.PasswordRef)
	if err != nil {
		return database.Config{}, fmt.Errorf("database.password_ref: %w", err)
	}
	if managed && raw.TLSMode != database.TLSVerifyFull {
		return database.Config{}, errors.New("managed database.tls_mode must be verify-full")
	}
	if !managed && raw.TLSMode != database.TLSDisable {
		return database.Config{}, errors.New("local database.tls_mode must be disable")
	}
	rootCA := ""
	if raw.TLSRootCAFile != "" {
		rootCA = absoluteFrom(filepath.Dir(configPath), raw.TLSRootCAFile)
	}
	connectTimeout, err := parsePositiveDuration("database.connect_timeout", raw.ConnectTimeout)
	if err != nil {
		return database.Config{}, err
	}
	statementTimeout, err := parsePositiveDuration("database.statement_timeout", raw.StatementTimeout)
	if err != nil {
		return database.Config{}, err
	}
	healthInterval, err := parsePositiveDuration("database.health_check_interval", raw.HealthCheckInterval)
	if err != nil {
		return database.Config{}, err
	}
	if raw.MaxConnections < 1 || raw.MaxConnections > 128 {
		return database.Config{}, errors.New("database.max_connections must be between 1 and 128")
	}
	if raw.Timezone != "UTC" {
		return database.Config{}, errors.New("database.timezone must be UTC")
	}
	return database.Config{
		Host:                raw.Host,
		Port:                raw.Port,
		Name:                raw.Name,
		User:                raw.User,
		Role:                raw.Role,
		PasswordRef:         passwordRef,
		TLSMode:             raw.TLSMode,
		TLSRootCAFile:       rootCA,
		ConnectTimeout:      connectTimeout,
		StatementTimeout:    statementTimeout,
		HealthCheckInterval: healthInterval,
		MaxConnections:      raw.MaxConnections,
		Timezone:            raw.Timezone,
	}, nil
}

func parseStorage(managed bool, awsRegion, configPath string, raw StorageFileConfig) (storage.Config, error) {
	if raw.AccessPurpose != AccessPurposeFoundationProof {
		return storage.Config{}, errors.New("object_storage.access_purpose must be foundation-proof")
	}
	if !managed {
		if raw.Provider != storage.ProviderFile || raw.Root == "" {
			return storage.Config{}, errors.New("local object_storage.provider must be filesystem with a root")
		}
		if raw.Region != "" || raw.Bucket != "" || raw.Prefix != "" || raw.ExpectedOwner != "" || raw.KMSKeyARN != "" {
			return storage.Config{}, errors.New("filesystem object storage cannot include S3 settings")
		}
		return storage.Config{
			Provider: raw.Provider, Root: absoluteFrom(filepath.Dir(configPath), raw.Root),
			AccessPurpose: raw.AccessPurpose,
		}, nil
	}
	if raw.Provider != storage.ProviderS3 {
		return storage.Config{}, errors.New("managed object_storage.provider must be s3")
	}
	if raw.Root != "" {
		return storage.Config{}, errors.New("S3 object storage cannot include a filesystem root")
	}
	if !awsRegionPattern.MatchString(raw.Region) || !s3BucketPattern.MatchString(raw.Bucket) ||
		!awsAccountPattern.MatchString(raw.ExpectedOwner) {
		return storage.Config{}, errors.New("S3 region, bucket, and expected_owner are invalid")
	}
	if raw.Region != awsRegion {
		return storage.Config{}, errors.New("object_storage.region must match the top-level aws_region")
	}
	if raw.Prefix == "" || strings.HasPrefix(raw.Prefix, "/") || strings.Contains(raw.Prefix, "..") {
		return storage.Config{}, errors.New("S3 prefix must be a non-empty relative prefix")
	}
	if err := validateKMSKeyARN(raw.KMSKeyARN, raw.Region, raw.ExpectedOwner); err != nil {
		return storage.Config{}, fmt.Errorf("object_storage.kms_key_arn: %w", err)
	}
	return storage.Config{
		Provider: raw.Provider, Region: raw.Region, Bucket: raw.Bucket, Prefix: raw.Prefix,
		ExpectedOwner: raw.ExpectedOwner, KMSKeyARN: raw.KMSKeyARN, AccessPurpose: raw.AccessPurpose,
	}, nil
}

func parseCrypto(managed bool, awsRegion string, raw CryptoFileConfig) (crypto.Config, error) {
	if strings.TrimSpace(raw.EncryptionContextPrefix) == "" {
		return crypto.Config{}, errors.New("kms.encryption_context_prefix is required")
	}
	if !managed {
		if raw.Provider != crypto.ProviderLocal {
			return crypto.Config{}, errors.New("local kms.provider must be local")
		}
		keyRef, err := secret.ParseRef(raw.KeyRef)
		if err != nil {
			return crypto.Config{}, fmt.Errorf("kms.key_ref: %w", err)
		}
		if raw.Region != "" || raw.KMSKeyARN != "" {
			return crypto.Config{}, errors.New("local KMS cannot include AWS KMS settings")
		}
		return crypto.Config{
			Provider: raw.Provider, KeyRef: keyRef, EncryptionContextPrefix: raw.EncryptionContextPrefix,
		}, nil
	}
	if raw.Provider != crypto.ProviderAWSKMS {
		return crypto.Config{}, errors.New("managed kms.provider must be aws_kms")
	}
	if raw.KeyRef != "" {
		return crypto.Config{}, errors.New("AWS KMS cannot include a local key reference")
	}
	if !awsRegionPattern.MatchString(raw.Region) {
		return crypto.Config{}, errors.New("kms.region is invalid")
	}
	if raw.Region != awsRegion {
		return crypto.Config{}, errors.New("kms.region must match the top-level aws_region")
	}
	if err := validateKMSKeyARN(raw.KMSKeyARN, raw.Region, ""); err != nil {
		return crypto.Config{}, fmt.Errorf("kms.kms_key_arn: %w", err)
	}
	return crypto.Config{
		Provider: raw.Provider, Region: raw.Region, KMSKeyARN: raw.KMSKeyARN,
		EncryptionContextPrefix: raw.EncryptionContextPrefix,
	}, nil
}

func parseTime(managed bool, raw TimeFileConfig) (TimeConfig, error) {
	wantProvider := TimeProviderSystem
	if managed {
		wantProvider = TimeProviderLinux
	}
	if raw.Provider != wantProvider {
		return TimeConfig{}, fmt.Errorf("time.provider must be %s for this deployment mode", wantProvider)
	}
	maxClockError, err := parsePositiveDuration("time.max_clock_error", raw.MaxClockError)
	if err != nil {
		return TimeConfig{}, err
	}
	maxDatabaseSkew, err := parsePositiveDuration("time.max_database_skew", raw.MaxDatabaseSkew)
	if err != nil {
		return TimeConfig{}, err
	}
	return TimeConfig{Provider: raw.Provider, MaxClockError: maxClockError, MaxDatabaseSkew: maxDatabaseSkew}, nil
}

func validateKMSKeyARN(value, region, account string) error {
	matches := awsKMSKeyARNPattern.FindStringSubmatch(value)
	if len(matches) != 3 || matches[1] != region || account != "" && matches[2] != account {
		return errors.New("must be a key ARN in the configured region and account")
	}
	return nil
}

func absoluteFrom(base, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(base, value)
}

func parsePositiveDuration(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", field)
	}
	return duration, nil
}
