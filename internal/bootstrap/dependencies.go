package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/acctbl/accountable/internal/platform/awsconfig"
	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/crypto"
	"github.com/acctbl/accountable/internal/platform/database"
	"github.com/acctbl/accountable/internal/platform/features"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/storage"
)

var ErrFoundationUnavailable = errors.New("foundation dependencies are unavailable")

type PreflightReport struct {
	BootstrapFlag features.FlagEvaluation
}

type Dependencies struct {
	Database      *database.Database
	Storage       storage.Storage
	Crypto        crypto.Crypto
	FeatureFlags  *features.FeatureFlags
	BootstrapFlag features.FlagEvaluation
	clock         clock.Clock
	timeHealth    clock.Health
	maximumDBSkew time.Duration
}

func Build(ctx context.Context, config Config) (*Dependencies, error) {
	timeSource := clock.Clock(clock.System{})
	timeHealth := clock.Health(clock.SystemHealth{})
	if config.Time.Provider == TimeProviderLinux {
		timeHealth = clock.NewLinuxHealth(config.Time.MaxClockError)
	}
	if err := timeHealth.Check(ctx); err != nil {
		return nil, err
	}

	var resolver secret.SecretSource
	var err error
	if config.Capabilities.Secrets {
		resolver, err = secret.NewSource(ctx, config.Secrets)
		if err != nil {
			return nil, ErrFoundationUnavailable
		}
	}
	var db *database.Database
	if config.Capabilities.Postgres {
		db, err = database.OpenDatabase(ctx, config.Database, resolver)
		if err != nil {
			return nil, err
		}
	}
	fail := func(err error) (*Dependencies, error) {
		if db != nil {
			db.Close()
		}
		return nil, err
	}
	var store storage.Storage
	if config.Capabilities.ObjectStorage {
		store, err = buildStorage(ctx, config)
		if err != nil {
			return fail(err)
		}
	}
	var cryptor crypto.Crypto
	if config.Capabilities.KMS {
		cryptor, err = buildCrypto(ctx, config, resolver)
		if err != nil {
			return fail(err)
		}
	}
	flags := features.NewFeatureFlags()
	dependencies := &Dependencies{
		Database: db, Storage: store, Crypto: cryptor,
		FeatureFlags: flags, BootstrapFlag: flags.BootstrapProbe(ctx),
		clock: timeSource, timeHealth: timeHealth, maximumDBSkew: config.Time.MaxDatabaseSkew,
	}
	checkCtx, cancel := context.WithTimeout(ctx, config.CheckTimeout)
	defer cancel()
	if err := dependencies.Check(checkCtx); err != nil {
		return fail(err)
	}
	return dependencies, nil
}

func (d *Dependencies) Check(ctx context.Context) error {
	if err := d.timeHealth.Check(ctx); err != nil {
		return err
	}
	if d.Database != nil {
		if err := d.Database.Check(ctx); err != nil {
			return err
		}
		if err := d.Database.CheckClock(ctx, d.clock, d.maximumDBSkew); err != nil {
			return err
		}
	}
	if d.Storage != nil {
		if err := d.Storage.Check(ctx); err != nil {
			return err
		}
	}
	if d.Crypto != nil {
		if err := d.Crypto.Check(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dependencies) Ping(ctx context.Context) error {
	if err := d.timeHealth.Check(ctx); err != nil {
		return err
	}
	if d.Database != nil {
		if err := d.Database.Ping(ctx); err != nil {
			return err
		}
	}
	if d.Storage != nil {
		if err := d.Storage.Probe(ctx); err != nil {
			return err
		}
	}
	if d.Crypto != nil {
		if err := d.Crypto.Probe(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dependencies) Close() {
	if d.Database != nil {
		d.Database.Close()
	}
}

func Preflight(ctx context.Context, config Config) (PreflightReport, error) {
	dependencies, err := Build(ctx, config)
	if err != nil {
		return PreflightReport{}, err
	}
	defer dependencies.Close()
	return PreflightReport{BootstrapFlag: dependencies.BootstrapFlag}, nil
}

func buildStorage(ctx context.Context, config Config) (storage.Storage, error) {
	if config.Storage.Provider == storage.ProviderFile {
		return storage.NewFileStorage(config.Storage.Root)
	}
	awsCfg, err := awsconfig.LoadConfig(ctx, config.Storage.Region)
	if err != nil {
		return nil, err
	}
	return storage.NewS3Storage(config.Storage, awsCfg), nil
}

func buildCrypto(ctx context.Context, config Config, secrets secret.SecretSource) (crypto.Crypto, error) {
	if config.Crypto.Provider == crypto.ProviderLocal {
		values, err := secrets.ResolveBatch(ctx, []secret.Ref{config.Crypto.KeyRef})
		if err != nil {
			return nil, crypto.ErrCryptoUnavailable
		}
		key, ok := values[config.Crypto.KeyRef]
		if !ok {
			return nil, crypto.ErrCryptoUnavailable
		}
		keyPath := ""
		if config.Secrets.Provider == secret.ProviderFile {
			keyPath = filepath.Join(config.Secrets.Directory, filepath.FromSlash(string(config.Crypto.KeyRef)))
		}
		return crypto.NewLocalCrypto(key, keyPath)
	}
	awsCfg, err := awsconfig.LoadConfig(ctx, config.Crypto.Region)
	if err != nil {
		return nil, err
	}
	return crypto.NewAWSCrypto(ctx, config.Crypto, awsCfg)
}
