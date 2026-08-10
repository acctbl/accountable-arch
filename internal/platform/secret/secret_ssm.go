package secret

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

var ErrSecretSourceUnavailable = errors.New("secret source is unavailable")

const parameterBatchLimit = 10

type parameterAPI interface {
	GetParameters(context.Context, *ssm.GetParametersInput, ...func(*ssm.Options)) (*ssm.GetParametersOutput, error)
}

type parameterWriterAPI interface {
	parameterAPI
	PutParameter(context.Context, *ssm.PutParameterInput, ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
}

type ParameterSecretSource struct {
	api  parameterAPI
	path string
}

type ParameterSecretStore struct {
	api    parameterWriterAPI
	path   string
	keyARN string
}

func NewParameterSecretSource(config Config, awsConfig aws.Config) *ParameterSecretSource {
	return &ParameterSecretSource{api: ssm.NewFromConfig(awsConfig), path: config.ParameterPath}
}

func newParameterSecretSource(api parameterAPI, path string) *ParameterSecretSource {
	return &ParameterSecretSource{api: api, path: path}
}

func NewParameterSecretStore(config Config, awsConfig aws.Config) *ParameterSecretStore {
	return &ParameterSecretStore{api: ssm.NewFromConfig(awsConfig), path: config.ParameterPath, keyARN: config.KMSKeyARN}
}

func newParameterSecretStore(api parameterWriterAPI, path, keyARN string) *ParameterSecretStore {
	return &ParameterSecretStore{api: api, path: path, keyARN: keyARN}
}

func (s *ParameterSecretSource) ResolveBatch(ctx context.Context, refs []Ref) (map[Ref]SecretValue, error) {
	if len(refs) == 0 {
		return map[Ref]SecretValue{}, nil
	}
	names, err := parameterNames(s.path, refs)
	if err != nil {
		return nil, err
	}
	return resolveParameters(ctx, s.api, refs, names)
}

func (s *ParameterSecretStore) ResolveOrCreateBatch(
	ctx context.Context,
	refs []Ref,
	generate func(Ref) ([]byte, error),
) (map[Ref]SecretValue, error) {
	if len(refs) == 0 || generate == nil || s.keyARN == "" {
		return nil, ErrSecretSourceUnavailable
	}
	names, err := parameterNames(s.path, refs)
	if err != nil {
		return nil, err
	}
	for index, ref := range refs {
		if err := s.create(ctx, ref, names[index], generate); err != nil {
			return nil, err
		}
	}
	return resolveParameters(ctx, s.api, refs, names)
}

// PutParameter without Overwrite fails when the parameter exists, so a concurrent
// bootstrap cannot replace a password that live database roles already use.
func (s *ParameterSecretStore) create(
	ctx context.Context,
	ref Ref,
	name string,
	generate func(Ref) ([]byte, error),
) error {
	value, err := generate(ref)
	defer clear(value)
	if err != nil || len(value) == 0 || len(value) > maximumSecretBytes {
		return ErrSecretSourceUnavailable
	}
	// Standard tier is encrypted directly under the key, which the writer's IAM
	// policy grants as kms:Encrypt. Moving to Advanced tier switches Parameter
	// Store to envelope encryption and requires kms:GenerateDataKey instead.
	_, err = s.api.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(name),
		Value:     aws.String(string(value)),
		Type:      types.ParameterTypeSecureString,
		KeyId:     aws.String(s.keyARN),
		Overwrite: aws.Bool(false),
		Tier:      types.ParameterTierStandard,
	})
	var exists *types.ParameterAlreadyExists
	if err != nil && !errors.As(err, &exists) {
		return ErrSecretSourceUnavailable
	}
	return nil
}

func resolveParameters(
	ctx context.Context,
	api parameterAPI,
	refs []Ref,
	names []string,
) (map[Ref]SecretValue, error) {
	resolved := make(map[string][]byte, len(names))
	for start := 0; start < len(names); start += parameterBatchLimit {
		end := min(start+parameterBatchLimit, len(names))
		output, err := api.GetParameters(ctx, &ssm.GetParametersInput{
			Names:          names[start:end],
			WithDecryption: aws.Bool(true),
		})
		if err != nil || output == nil || len(output.InvalidParameters) > 0 {
			return nil, ErrSecretSourceUnavailable
		}
		for _, parameter := range output.Parameters {
			if parameter.Name == nil || parameter.Value == nil || parameter.Type != types.ParameterTypeSecureString {
				return nil, ErrSecretSourceUnavailable
			}
			resolved[*parameter.Name] = []byte(*parameter.Value)
		}
	}
	values := make(map[Ref]SecretValue, len(refs))
	for index, ref := range refs {
		value, ok := resolved[names[index]]
		if !ok || len(value) == 0 || len(value) > maximumSecretBytes {
			return nil, ErrSecretSourceUnavailable
		}
		values[ref] = NewSecretValue(value)
		clear(value)
	}
	if len(values) != len(refs) {
		return nil, ErrSecretSourceUnavailable
	}
	return values, nil
}

func parameterNames(path string, refs []Ref) ([]string, error) {
	if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return nil, ErrSecretSourceUnavailable
	}
	names := make([]string, 0, len(refs))
	seen := make(map[Ref]struct{}, len(refs))
	for _, ref := range refs {
		parsed, err := ParseRef(string(ref))
		if err != nil {
			return nil, ErrSecretSourceUnavailable
		}
		if _, duplicate := seen[ref]; duplicate {
			return nil, ErrSecretSourceUnavailable
		}
		seen[ref] = struct{}{}
		names = append(names, path+"/"+string(parsed))
	}
	return names, nil
}
