module github.com/acctbl/accountable

go 1.26.5

require (
	connectrpc.com/connect v1.20.0
	github.com/aws/aws-cryptographic-material-providers-library/releases/go/mpl v0.4.0
	github.com/aws/aws-encryption-sdk/releases/go/encryption-sdk v0.4.0
	github.com/aws/aws-lambda-go v1.52.0
	github.com/aws/aws-sdk-go-v2 v1.43.5
	github.com/aws/aws-sdk-go-v2/config v1.32.34
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.82.1
	github.com/aws/aws-sdk-go-v2/service/kms v1.55.3
	github.com/aws/aws-sdk-go-v2/service/s3 v1.106.4
	github.com/aws/aws-sdk-go-v2/service/sns v1.42.5
	github.com/aws/aws-sdk-go-v2/service/ssm v1.73.4
	github.com/google/uuid v1.6.0
	github.com/gowebpki/jcs v1.0.1
	github.com/jackc/pgx/v5 v5.10.0
	github.com/open-feature/go-sdk v1.17.2
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/pressly/goose/v3 v3.27.3
	go.opentelemetry.io/collector/component v1.64.0
	go.opentelemetry.io/collector/config/confighttp v0.158.0
	go.opentelemetry.io/collector/config/configoptional v1.64.0
	go.opentelemetry.io/collector/config/configretry v1.64.0
	go.opentelemetry.io/collector/consumer v1.64.0
	go.opentelemetry.io/collector/consumer/consumererror v0.158.0
	go.opentelemetry.io/collector/exporter v1.64.0
	go.opentelemetry.io/collector/exporter/exporterhelper v0.158.0
	go.opentelemetry.io/collector/exporter/exportertest v0.158.0
	go.opentelemetry.io/collector/pdata v1.64.0
	go.opentelemetry.io/contrib/bridges/otelslog v0.19.0
	go.opentelemetry.io/otel v1.45.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.20.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.45.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.45.0
	go.opentelemetry.io/otel/log v0.20.0
	go.opentelemetry.io/otel/metric v1.45.0
	go.opentelemetry.io/otel/sdk v1.45.0
	go.opentelemetry.io/otel/sdk/log v0.20.0
	go.opentelemetry.io/otel/sdk/metric v1.45.0
	go.opentelemetry.io/otel/trace v1.45.0
	golang.org/x/sys v0.47.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/aws/aws-cryptographic-material-providers-library/releases/go/dynamodb v0.4.0 // indirect
	github.com/aws/aws-cryptographic-material-providers-library/releases/go/kms v0.4.0 // indirect
	github.com/aws/aws-cryptographic-material-providers-library/releases/go/primitives v0.4.0 // indirect
	github.com/aws/aws-cryptographic-material-providers-library/releases/go/smithy-dafny-standard-library v0.4.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.17 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.33 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.36 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.36 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.35 // indirect
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.66.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.57.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.27 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.11.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.35 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.3 // indirect
	github.com/aws/smithy-go v1.27.7 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cenkalti/backoff/v7 v7.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dafny-lang/DafnyRuntimeGo/v4 v4.11.3 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/foxboron/go-tpm-keyfiles v0.0.0-20251226215517-609e4778396f // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/hashicorp/go-version v1.9.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/knadh/koanf/providers/confmap v1.0.0 // indirect
	github.com/knadh/koanf/v2 v2.3.5 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/collector/client v1.64.0 // indirect
	go.opentelemetry.io/collector/component/componenttest v0.158.0 // indirect
	go.opentelemetry.io/collector/config/configauth v1.64.0 // indirect
	go.opentelemetry.io/collector/config/configcompression v1.64.0 // indirect
	go.opentelemetry.io/collector/config/configmiddleware v1.64.0 // indirect
	go.opentelemetry.io/collector/config/confignet v1.64.0 // indirect
	go.opentelemetry.io/collector/config/configopaque v1.64.0 // indirect
	go.opentelemetry.io/collector/config/configtls v1.64.0 // indirect
	go.opentelemetry.io/collector/confmap v1.64.0 // indirect
	go.opentelemetry.io/collector/consumer/consumertest v0.158.0 // indirect
	go.opentelemetry.io/collector/consumer/xconsumer v0.158.0 // indirect
	go.opentelemetry.io/collector/exporter/xexporter v0.158.0 // indirect
	go.opentelemetry.io/collector/extension v1.64.0 // indirect
	go.opentelemetry.io/collector/extension/extensionauth v1.64.0 // indirect
	go.opentelemetry.io/collector/extension/extensionmiddleware v0.158.0 // indirect
	go.opentelemetry.io/collector/extension/xextension v0.158.0 // indirect
	go.opentelemetry.io/collector/featuregate v1.64.0 // indirect
	go.opentelemetry.io/collector/internal/componentalias v0.158.0 // indirect
	go.opentelemetry.io/collector/pdata/pprofile v0.158.0 // indirect
	go.opentelemetry.io/collector/pdata/xpdata v0.158.0 // indirect
	go.opentelemetry.io/collector/pipeline v1.64.0 // indirect
	go.opentelemetry.io/collector/pipeline/xpipeline v0.158.0 // indirect
	go.opentelemetry.io/collector/receiver v1.64.0 // indirect
	go.opentelemetry.io/collector/receiver/receivertest v0.158.0 // indirect
	go.opentelemetry.io/collector/receiver/xreceiver v0.158.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.45.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/mock v0.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/grpc v1.83.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
