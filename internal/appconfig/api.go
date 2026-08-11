package appconfig

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"time"

	"github.com/acctbl/accountable/internal/bootstrap"
	"github.com/acctbl/accountable/internal/configfile"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

const HTTPWriteTimeout = 30 * time.Second

type API struct {
	Addr                    string
	Environment             string
	ArchitectureProbe       bool
	AllowedOrigins          []string
	TrustedProxies          []*net.IPNet
	TLSCertFile             string
	TLSKeyFile              string
	UnaryRPCDeadline        time.Duration
	StreamRPCDeadline       time.Duration
	Foundation              bootstrap.Config
	ClientTelemetry         telemetry.ClientIdentityTuple
	PreviousClientTelemetry *telemetry.ClientIdentityTuple
	ProofKeyActiveRef       string
	ProofKeyStandbyRef      string
}

type APIFile struct {
	bootstrap.FileConfig
	Server          *ServerFileConfig          `toml:"server"`
	ClientTelemetry *ClientTelemetryFileConfig `toml:"client_telemetry"`
}

type ClientTelemetryFileConfig struct {
	ReleaseID                  string                    `toml:"release_id"`
	WebAssetDigest             string                    `toml:"web_asset_digest"`
	ConfigurationSchemaVersion int                       `toml:"configuration_schema_version"`
	ConfigurationRevision      string                    `toml:"configuration_revision"`
	ConfigurationFingerprint   string                    `toml:"configuration_fingerprint"`
	ProofKeyActiveRef          string                    `toml:"proof_key_active_ref"`
	ProofKeyStandbyRef         string                    `toml:"proof_key_standby_ref"`
	Previous                   *ClientIdentityFileConfig `toml:"previous"`
}

type ClientIdentityFileConfig struct {
	ReleaseID                  string `toml:"release_id"`
	WebAssetDigest             string `toml:"web_asset_digest"`
	ConfigurationSchemaVersion int    `toml:"configuration_schema_version"`
	ConfigurationRevision      string `toml:"configuration_revision"`
	ConfigurationFingerprint   string `toml:"configuration_fingerprint"`
}

type ServerFileConfig struct {
	ListenAddress     string   `toml:"listen_address"`
	AllowedOrigins    []string `toml:"allowed_origins"`
	TrustedProxyCIDRs []string `toml:"trusted_proxy_cidrs"`
	TLSCertificate    string   `toml:"tls_certificate_file"`
	TLSPrivateKey     string   `toml:"tls_private_key_file"`
	UnaryRPCTimeout   string   `toml:"unary_rpc_timeout"`
	StreamRPCTimeout  string   `toml:"stream_rpc_timeout"`
}

// RuntimePaths reports the configuration and release paths that every managed
// runtime receives. Release identity always arrives beside the configuration and
// never inside it.
func RuntimePaths(args []string) (configPath string, releasePath string, err error) {
	paths, err := configfile.AbsolutePaths(args, "--config", "--release")
	if err != nil {
		return "", "", err
	}
	return paths["--config"], paths["--release"], nil
}

// LoadRuntime reads the configuration and the verified release document for one
// managed runtime role.
func LoadRuntime(args []string, expectedRole string) (bootstrap.Config, telemetry.RuntimeRelease, error) {
	configPath, releasePath, err := RuntimePaths(args)
	if err != nil {
		return bootstrap.Config{}, telemetry.RuntimeRelease{}, err
	}
	release, err := telemetry.ReadRuntimeRelease(releasePath)
	if err != nil {
		return bootstrap.Config{}, telemetry.RuntimeRelease{}, err
	}
	config, err := LoadFoundation([]string{"--config", configPath}, expectedRole)
	if err != nil {
		return bootstrap.Config{}, telemetry.RuntimeRelease{}, err
	}
	return config, release, nil
}

func LoadAPI(args []string) (API, error) {
	raw, foundationConfig, err := loadRuntime(args, bootstrap.RuntimeRoleAPI)
	if err != nil {
		return API{}, err
	}
	return parseAPI(raw, foundationConfig)
}

func parseAPI(raw APIFile, foundationConfig bootstrap.Config) (API, error) {
	server := raw.Server
	if server == nil {
		return API{}, errors.New("server section is required when runtime_role is api")
	}
	if raw.ClientTelemetry == nil {
		return API{}, errors.New("client_telemetry section is required when runtime_role is api")
	}
	clientIdentity := telemetry.ClientIdentityTuple{
		Application: "web", ReleaseID: raw.ClientTelemetry.ReleaseID,
		ArtifactType: "web_assets", ArtifactDigest: raw.ClientTelemetry.WebAssetDigest,
		ConfigurationSchemaVersion: raw.ClientTelemetry.ConfigurationSchemaVersion,
		ConfigurationRevision:      raw.ClientTelemetry.ConfigurationRevision,
		ConfigurationFingerprint:   raw.ClientTelemetry.ConfigurationFingerprint,
	}
	if err := telemetry.ValidateClientIdentityTuple(clientIdentity); err != nil {
		return API{}, err
	}
	var previousClientIdentity *telemetry.ClientIdentityTuple
	if previous := raw.ClientTelemetry.Previous; previous != nil {
		identity := telemetry.ClientIdentityTuple{
			Application: "web", ReleaseID: previous.ReleaseID,
			ArtifactType: "web_assets", ArtifactDigest: previous.WebAssetDigest,
			ConfigurationSchemaVersion: previous.ConfigurationSchemaVersion,
			ConfigurationRevision:      previous.ConfigurationRevision,
			ConfigurationFingerprint:   previous.ConfigurationFingerprint,
		}
		if err := telemetry.ValidateClientIdentityTuple(identity); err != nil || identity == clientIdentity {
			return API{}, telemetry.ErrInvalidContract
		}
		previousClientIdentity = &identity
	}
	if raw.ClientTelemetry.ProofKeyActiveRef == "" {
		return API{}, errors.New("client_telemetry active proof key reference is required")
	}
	if server.ListenAddress == "" {
		return API{}, errors.New("server.listen_address is required")
	}
	if _, _, err := net.SplitHostPort(server.ListenAddress); err != nil {
		return API{}, fmt.Errorf("server.listen_address: %w", err)
	}
	if server.AllowedOrigins == nil {
		return API{}, errors.New("server.allowed_origins is required")
	}
	if err := validateAllowedOrigins(foundationConfig.Environment, server.AllowedOrigins); err != nil {
		return API{}, err
	}
	if server.TrustedProxyCIDRs == nil {
		return API{}, errors.New("server.trusted_proxy_cidrs is required")
	}
	if (server.TLSCertificate == "") != (server.TLSPrivateKey == "") {
		return API{}, errors.New("server.tls_certificate_file and server.tls_private_key_file must be configured together")
	}
	if server.TLSCertificate != "" && !filepath.IsAbs(server.TLSCertificate) {
		return API{}, errors.New("server.tls_certificate_file must be an absolute path")
	}
	if server.TLSPrivateKey != "" && !filepath.IsAbs(server.TLSPrivateKey) {
		return API{}, errors.New("server.tls_private_key_file must be an absolute path")
	}
	if server.TLSCertificate != "" {
		if _, err := tls.LoadX509KeyPair(server.TLSCertificate, server.TLSPrivateKey); err != nil {
			return API{}, errors.New("TLS identity is unavailable")
		}
	}
	unaryDeadline, err := positiveDuration("server.unary_rpc_timeout", server.UnaryRPCTimeout)
	if err != nil {
		return API{}, err
	}
	streamDeadline, err := positiveDuration("server.stream_rpc_timeout", server.StreamRPCTimeout)
	if err != nil {
		return API{}, err
	}
	if unaryDeadline >= HTTPWriteTimeout {
		return API{}, errors.New("server.unary_rpc_timeout must be shorter than the HTTP write timeout")
	}
	if streamDeadline >= HTTPWriteTimeout {
		return API{}, errors.New("server.stream_rpc_timeout must be shorter than the HTTP write timeout")
	}
	trusted, err := parseCIDRs(server.TrustedProxyCIDRs)
	if err != nil {
		return API{}, fmt.Errorf("server.trusted_proxy_cidrs: %w", err)
	}
	return API{
		Addr: server.ListenAddress, Environment: foundationConfig.Environment,
		ArchitectureProbe: foundationConfig.Capabilities.ArchitectureProbe,
		AllowedOrigins:    server.AllowedOrigins, TrustedProxies: trusted,
		TLSCertFile: server.TLSCertificate, TLSKeyFile: server.TLSPrivateKey,
		UnaryRPCDeadline: unaryDeadline, StreamRPCDeadline: streamDeadline, Foundation: foundationConfig,
		ClientTelemetry:         clientIdentity,
		PreviousClientTelemetry: previousClientIdentity,
		ProofKeyActiveRef:       raw.ClientTelemetry.ProofKeyActiveRef,
		ProofKeyStandbyRef:      raw.ClientTelemetry.ProofKeyStandbyRef,
	}, nil
}

func LoadFoundation(args []string, expectedRole string) (bootstrap.Config, error) {
	raw, config, err := loadRuntime(args, expectedRole)
	if err == nil && expectedRole == "" && config.RuntimeRole == bootstrap.RuntimeRoleAPI {
		apiConfig, apiErr := parseAPI(raw, config)
		if apiErr != nil {
			return bootstrap.Config{}, apiErr
		}
		return apiConfig.Foundation, nil
	}
	return config, err
}

func loadRuntime(args []string, expectedRole string) (APIFile, bootstrap.Config, error) {
	path, err := configfile.AbsolutePath(args)
	if err != nil {
		return APIFile{}, bootstrap.Config{}, err
	}
	var raw APIFile
	fingerprint, err := configfile.DecodeWithFingerprint(path, &raw)
	if err != nil {
		return APIFile{}, bootstrap.Config{}, err
	}
	foundationConfig, err := bootstrap.Parse(path, fingerprint, raw.FileConfig)
	if err != nil {
		return APIFile{}, bootstrap.Config{}, err
	}
	if expectedRole != "" && foundationConfig.RuntimeRole != expectedRole {
		return APIFile{}, bootstrap.Config{}, fmt.Errorf("runtime_role must be %s for this binary", expectedRole)
	}
	if foundationConfig.RuntimeRole == bootstrap.RuntimeRoleMigrate &&
		(!foundationConfig.Capabilities.Secrets || !foundationConfig.Capabilities.Postgres) {
		return APIFile{}, bootstrap.Config{}, errors.New("runtime_role migrate requires secrets and postgres capabilities")
	}
	if foundationConfig.RuntimeRole != bootstrap.RuntimeRoleAPI && raw.Server != nil {
		return APIFile{}, bootstrap.Config{}, errors.New("server section is only valid when runtime_role is api")
	}
	return raw, foundationConfig, nil
}

func validateAllowedOrigins(environment string, values []string) error {
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("allowed_origins must contain absolute HTTP origins without credentials, paths, queries, or fragments")
		}
		if environment == "production" && parsed.Scheme != "https" {
			return errors.New("production allowed_origins must use HTTPS")
		}
	}
	return nil
}

func positiveDuration(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", field)
	}
	return duration, nil
}

func parseCIDRs(values []string) ([]*net.IPNet, error) {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		networks = append(networks, network)
	}
	return networks, nil
}
