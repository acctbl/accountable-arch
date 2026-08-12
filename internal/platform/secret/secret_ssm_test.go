package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type fakeParameterAPI struct {
	values    map[string]string
	plaintext map[string]bool
	put       map[string]string
	requested [][]string
	decrypted []bool
	putInputs []*ssm.PutParameterInput
	err       error
}

func newFakeParameterAPI(values map[string]string) *fakeParameterAPI {
	return &fakeParameterAPI{
		values:    values,
		plaintext: make(map[string]bool),
		put:       make(map[string]string),
	}
}

func (f *fakeParameterAPI) GetParameters(
	_ context.Context,
	input *ssm.GetParametersInput,
	_ ...func(*ssm.Options),
) (*ssm.GetParametersOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.requested = append(f.requested, input.Names)
	f.decrypted = append(f.decrypted, aws.ToBool(input.WithDecryption))
	output := &ssm.GetParametersOutput{}
	for _, name := range input.Names {
		value, ok := f.values[name]
		if !ok {
			output.InvalidParameters = append(output.InvalidParameters, name)
			continue
		}
		parameterType := types.ParameterTypeSecureString
		if f.plaintext[name] {
			parameterType = types.ParameterTypeString
		}
		output.Parameters = append(output.Parameters, types.Parameter{
			Name: aws.String(name), Value: aws.String(value), Type: parameterType,
		})
	}
	return output, nil
}

func (f *fakeParameterAPI) PutParameter(
	_ context.Context,
	input *ssm.PutParameterInput,
	_ ...func(*ssm.Options),
) (*ssm.PutParameterOutput, error) {
	f.putInputs = append(f.putInputs, input)
	name := aws.ToString(input.Name)
	if _, exists := f.values[name]; exists {
		return nil, &types.ParameterAlreadyExists{}
	}
	f.put[name] = aws.ToString(input.Value)
	f.values[name] = aws.ToString(input.Value)
	return &ssm.PutParameterOutput{}, nil
}

func TestParameterSecretSourceResolvesCompleteBatchUnderThePath(t *testing.T) {
	t.Parallel()

	api := newFakeParameterAPI(map[string]string{
		"/accountable/development-01/development/api/database-password": "database-secret",
		"/accountable/development-01/development/api/crypto/key":        "crypto-secret",
	})
	source := newParameterSecretSource(api, "/accountable/development-01/development/api")
	values, err := source.ResolveBatch(context.Background(), []Ref{"database-password", "crypto/key"})
	if err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if got := string(values["database-password"].Bytes()); got != "database-secret" {
		t.Fatalf("database secret = %q", got)
	}
	if got := string(values["crypto/key"].Bytes()); got != "crypto-secret" {
		t.Fatalf("crypto secret = %q", got)
	}
	if len(api.decrypted) != 1 || !api.decrypted[0] {
		t.Fatalf("WithDecryption = %v; want a single decrypting request", api.decrypted)
	}
}

func TestParameterSecretSourceReturnsNoPartialBatch(t *testing.T) {
	t.Parallel()

	api := newFakeParameterAPI(map[string]string{"/cell/api/database-password": "database-secret"})
	source := newParameterSecretSource(api, "/cell/api")
	values, err := source.ResolveBatch(context.Background(), []Ref{"database-password", "missing"})
	if !errors.Is(err, ErrSecretSourceUnavailable) || values != nil {
		t.Fatalf("ResolveBatch = %#v, %v; want nil, unavailable", values, err)
	}
}

func TestParameterSecretSourceTreatsOnlyAMissingOptionalParameterAsAbsent(t *testing.T) {
	t.Parallel()

	api := newFakeParameterAPI(map[string]string{"/cell/api/standby": "standby-secret"})
	source := newParameterSecretSource(api, "/cell/api")
	value, present, err := source.ResolveOptional(context.Background(), "standby")
	if err != nil || !present || string(value.Bytes()) != "standby-secret" {
		t.Fatalf("present optional parameter = %q, %t, %v", value.Bytes(), present, err)
	}
	_, present, err = source.ResolveOptional(context.Background(), "missing")
	if err != nil || present {
		t.Fatalf("missing optional parameter = %t, %v", present, err)
	}
	api.err = errors.New("service unavailable")
	if _, _, err := source.ResolveOptional(context.Background(), "standby"); !errors.Is(err, ErrSecretSourceUnavailable) {
		t.Fatalf("failed optional lookup = %v", err)
	}
}

func TestParameterSecretSourceRejectsPlaintextParameters(t *testing.T) {
	t.Parallel()

	api := newFakeParameterAPI(map[string]string{"/cell/api/database-password": "database-secret"})
	api.plaintext["/cell/api/database-password"] = true
	source := newParameterSecretSource(api, "/cell/api")
	if values, err := source.ResolveBatch(context.Background(), []Ref{"database-password"}); err == nil || values != nil {
		t.Fatalf("plaintext ResolveBatch = %#v, %v; want nil, unavailable", values, err)
	}
}

func TestParameterSecretSourceRefusesDuplicateOversizedAndEscapingReferences(t *testing.T) {
	t.Parallel()

	duplicate := newParameterSecretSource(newFakeParameterAPI(nil), "/cell/api")
	if values, err := duplicate.ResolveBatch(context.Background(), []Ref{"same", "same"}); err == nil || values != nil {
		t.Fatalf("duplicate ResolveBatch = %#v, %v", values, err)
	}

	oversized := newParameterSecretSource(newFakeParameterAPI(map[string]string{
		"/cell/api/large": strings.Repeat("x", maximumSecretBytes+1),
	}), "/cell/api")
	if values, err := oversized.ResolveBatch(context.Background(), []Ref{"large"}); err == nil || values != nil {
		t.Fatalf("oversized ResolveBatch = %#v, %v", values, err)
	}

	escaping := newParameterSecretSource(newFakeParameterAPI(nil), "/cell/api")
	if values, err := escaping.ResolveBatch(context.Background(), []Ref{"../migrate/database-password"}); err == nil || values != nil {
		t.Fatalf("escaping ResolveBatch = %#v, %v", values, err)
	}

	relative := newParameterSecretSource(newFakeParameterAPI(nil), "cell/api")
	if values, err := relative.ResolveBatch(context.Background(), []Ref{"database-password"}); err == nil || values != nil {
		t.Fatalf("relative-path ResolveBatch = %#v, %v", values, err)
	}
}

func TestParameterSecretStoreCreatesOnlyMissingValuesAsSecureString(t *testing.T) {
	t.Parallel()

	api := newFakeParameterAPI(map[string]string{"/cell/api/database/existing": "existing"})
	store := newParameterSecretStore(api, "/cell/api", "arn:aws:kms:eu-west-2:000000000000:key/cell")
	values, err := store.ResolveOrCreateBatch(
		context.Background(),
		[]Ref{"database/existing", "database/missing"},
		func(ref Ref) ([]byte, error) { return []byte("generated-for-" + string(ref)), nil },
	)
	if err != nil {
		t.Fatalf("ResolveOrCreateBatch: %v", err)
	}
	if got := string(values["database/existing"].Bytes()); got != "existing" {
		t.Fatalf("existing value = %q", got)
	}
	if got := string(values["database/missing"].Bytes()); got != "generated-for-database/missing" {
		t.Fatalf("created value = %q", got)
	}
	if len(api.put) != 1 || api.put["/cell/api/database/missing"] != "generated-for-database/missing" {
		t.Fatalf("put = %#v", api.put)
	}
	for _, input := range api.putInputs {
		if input.Type != types.ParameterTypeSecureString || aws.ToBool(input.Overwrite) ||
			aws.ToString(input.KeyId) != "arn:aws:kms:eu-west-2:000000000000:key/cell" {
			t.Fatalf("unsafe put: type=%s overwrite=%v key=%q",
				input.Type, aws.ToBool(input.Overwrite), aws.ToString(input.KeyId))
		}
	}
}

// A losing bootstrap must adopt the winner's value rather than overwrite the
// password that live database roles already authenticate with.
func TestParameterSecretStoreAdoptsTheExistingValueOnConflict(t *testing.T) {
	t.Parallel()

	api := newFakeParameterAPI(map[string]string{"/cell/api/database-password": "written-by-the-winner"})
	store := newParameterSecretStore(api, "/cell/api", "arn:aws:kms:eu-west-2:000000000000:key/cell")
	values, err := store.ResolveOrCreateBatch(context.Background(), []Ref{"database-password"}, func(Ref) ([]byte, error) {
		return []byte("would-have-clobbered"), nil
	})
	if err != nil {
		t.Fatalf("ResolveOrCreateBatch: %v", err)
	}
	if got := string(values["database-password"].Bytes()); got != "written-by-the-winner" {
		t.Fatalf("resolved value = %q; want the existing value", got)
	}
	if len(api.put) != 0 {
		t.Fatalf("put = %#v; want no write", api.put)
	}
}

func TestParameterSecretStoreRequiresACustomerManagedKey(t *testing.T) {
	t.Parallel()

	store := newParameterSecretStore(newFakeParameterAPI(nil), "/cell/api", "")
	values, err := store.ResolveOrCreateBatch(context.Background(), []Ref{"database-password"}, func(Ref) ([]byte, error) {
		return []byte("generated"), nil
	})
	if !errors.Is(err, ErrSecretSourceUnavailable) || values != nil {
		t.Fatalf("ResolveOrCreateBatch = %#v, %v; want nil, unavailable", values, err)
	}
}

func TestParameterSecretSourceSplitsBatchesAtTheAPILimit(t *testing.T) {
	t.Parallel()

	values := make(map[string]string, parameterBatchLimit+1)
	refs := make([]Ref, 0, parameterBatchLimit+1)
	for index := range parameterBatchLimit + 1 {
		ref := Ref("secret-" + string(rune('a'+index)))
		refs = append(refs, ref)
		values["/cell/api/"+string(ref)] = "value-" + string(ref)
	}
	api := newFakeParameterAPI(values)
	source := newParameterSecretSource(api, "/cell/api")
	resolved, err := source.ResolveBatch(context.Background(), refs)
	if err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(resolved) != len(refs) {
		t.Fatalf("resolved %d values; want %d", len(resolved), len(refs))
	}
	if len(api.requested) != 2 || len(api.requested[0]) != parameterBatchLimit || len(api.requested[1]) != 1 {
		t.Fatalf("requests = %#v; want batches of %d then 1", api.requested, parameterBatchLimit)
	}
}
