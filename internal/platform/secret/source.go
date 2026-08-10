package secret

import (
	"context"

	"github.com/acctbl/accountable/internal/platform/awsconfig"
)

type SecretStore interface {
	ResolveOrCreateBatch(context.Context, []Ref, func(Ref) ([]byte, error)) (map[Ref]SecretValue, error)
}

func NewSource(ctx context.Context, config Config) (SecretSource, error) {
	if config.Provider == ProviderFile {
		return NewFileSecretSource(config.Directory)
	}
	awsCfg, err := awsconfig.LoadConfig(ctx, config.AWSRegion)
	if err != nil {
		return nil, err
	}
	return NewParameterSecretSource(config, awsCfg), nil
}

func NewStore(ctx context.Context, config Config) (SecretStore, error) {
	if config.Provider != ProviderParameterStore {
		return nil, ErrSecretSourceUnavailable
	}
	awsCfg, err := awsconfig.LoadConfig(ctx, config.AWSRegion)
	if err != nil {
		return nil, err
	}
	return NewParameterSecretStore(config, awsCfg), nil
}
