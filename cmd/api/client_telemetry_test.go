package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

type recordingNonceClaimer struct{ claimed bool }

func (claimer *recordingNonceClaimer) ClaimProofNonce(context.Context, string, time.Time) (bool, error) {
	if claimer.claimed {
		return false, nil
	}
	claimer.claimed = true
	return true, nil
}

func TestClientTelemetryShutdownDrainsEveryAdmittedEvent(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	release := telemetry.RuntimeRelease{
		SchemaVersion:         telemetry.ContractSchemaVersion,
		ReleaseID:             "018f47d2-7f00-7abc-9def-1234567890ab",
		ReleaseManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ServiceVersion:        "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		WebAssetDigest:        "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	configIdentity := telemetry.ConfigIdentity{
		SchemaVersion: 1,
		Revision:      "shutdown-proof",
		Fingerprint:   "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{
		Identity: telemetry.RuntimeIdentity{
			Release: release, Config: configIdentity, Environment: "development", CellID: "development-01", RuntimeRole: "api",
		},
		Config: telemetry.RuntimeConfig{
			CollectorEndpoint:      "http://127.0.0.1:4318",
			Protocol:               telemetry.TelemetryProtocol,
			SamplingPolicy:         telemetry.TelemetrySamplingPolicy,
			RedactionPolicyVersion: telemetry.TelemetryRedactionPolicy,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = providers.Shutdown(ctx)
	})
	recorder, err := telemetry.NewRecorder(providers)
	if err != nil {
		t.Fatal(err)
	}
	processResource, err := telemetry.BuildProcessResource(telemetry.RuntimeIdentity{
		Release: release, Config: configIdentity, Environment: "development", CellID: "development-01", RuntimeRole: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := telemetry.NewSafeLogger(telemetry.SafeLoggerOptions{
		Stdout: &logs, Resource: processResource, Clock: clock.Fixed{Instant: instant},
	})
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	identity := telemetry.ClientIdentityTuple{
		Application: "web", ReleaseID: release.ReleaseID, ArtifactType: "web_assets", ArtifactDigest: release.WebAssetDigest,
		ConfigurationSchemaVersion: 1, ConfigurationRevision: configIdentity.Revision,
		ConfigurationFingerprint: configIdentity.Fingerprint,
	}
	service, err := newClientTelemetryService(
		identity, nil, release, recorder, logger, key, "", &recordingNonceClaimer{}, clock.Fixed{Instant: instant},
	)
	if err != nil {
		t.Fatal(err)
	}
	service.queue <- admittedClientEvent{
		identity: telemetry.WorkIdentity{
			RequestID:     "018f47d2-7f00-7abc-8def-1234567890ab",
			CorrelationID: "018f47d2-7f00-7abc-8def-1234567890ac",
			EventID:       "018f47d2-7f00-7abc-8def-1234567890ad",
		},
		envelope: telemetry.ClientEnvelope{
			SchemaVersion: 1, ClientApplication: "web", EventName: "app.ready",
			OccurredAt: "2026-08-11T10:00:00.000Z", CorrelationID: "018f47d2-7f00-7abc-8def-1234567890ac",
			RouteClass: "home", ReleaseID: release.ReleaseID,
			ClientArtifactType: "web_assets", ClientArtifactDigest: release.WebAssetDigest,
			ConfigurationSchemaVersion: 1, ConfigurationRevision: configIdentity.Revision,
			ConfigurationFingerprint: configIdentity.Fingerprint,
			Attributes:               map[string]any{"duration_ms": json.Number("1")},
		},
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"event_name":"client.event"`) {
		t.Fatalf("accepted event was not processed: %s", logs.String())
	}
	if strings.Contains(logs.String(), "telemetry.client_event_dropped") {
		t.Fatalf("graceful drain invented a drop: %s", logs.String())
	}

	handler := clientTelemetryBoundary(clock.Fixed{Instant: instant}, service.Handle([]string{"https://shell.example"}))
	request := httptest.NewRequest(http.MethodPost, "/telemetry/v1", io.NopCloser(strings.NewReader(`{}`)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://shell.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusAccepted {
		t.Fatal("stopped admission accepted a new event")
	}
}

func TestClientTelemetryAdmissionRequiresTheCompleteTrustedBoundary(t *testing.T) {
	now := time.Now().UTC()
	correlationID := "019261bb-4c10-7f3a-9aaa-0f1e2d3c4b5a"
	active := telemetry.ClientIdentityTuple{
		Application: "web", ReleaseID: "018f47d2-7f00-7abc-9def-1234567890ab",
		ArtifactType: "web_assets", ArtifactDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ConfigurationSchemaVersion: 1, ConfigurationRevision: "test",
		ConfigurationFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	raw, err := json.Marshal(map[string]any{
		"schema_version": 1, "client_application": "web", "event_name": "app.ready",
		"occurred_at": now.Format("2006-01-02T15:04:05.000Z"), "correlation_id": correlationID,
		"route_class": "home", "release_id": active.ReleaseID,
		"client_artifact_type": "web_assets", "client_artifact_digest": active.ArtifactDigest,
		"configuration_schema_version": 1, "configuration_revision": "test",
		"configuration_fingerprint": active.ConfigurationFingerprint,
		"attributes":                map[string]any{"duration_ms": 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*http.Request){
		"accepted":     func(*http.Request) {},
		"wrong origin": func(request *http.Request) { request.Header.Set("Origin", "https://attacker.example") },
		"compressed":   func(request *http.Request) { request.Header.Set("Content-Encoding", "gzip") },
		"correlation substitution": func(request *http.Request) {
			request.Header.Set(telemetry.HeaderCorrelationID, "018f47d2-7f00-7abc-9def-1234567890ac")
		},
	} {
		t.Run(name, func(t *testing.T) {
			service := &clientTelemetryService{active: active, queue: make(chan admittedClientEvent, 1)}
			handler := clientTelemetryBoundary(clock.Fixed{Instant: now}, service.Handle([]string{"https://shell.example"}))
			request := httptest.NewRequest(http.MethodPost, "/telemetry/v1", bytes.NewReader(raw))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "https://shell.example")
			request.Header.Set(telemetry.HeaderCorrelationID, correlationID)
			mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if name == "accepted" {
				if response.Code != http.StatusAccepted || response.Body.Len() != 0 || len(service.queue) != 1 {
					t.Fatalf("status=%d body=%q queue=%d", response.Code, response.Body.String(), len(service.queue))
				}
				busyRequest := httptest.NewRequest(http.MethodPost, "/telemetry/v1", bytes.NewReader(raw))
				busyRequest.Header = request.Header.Clone()
				busyResponse := httptest.NewRecorder()
				handler.ServeHTTP(busyResponse, busyRequest)
				if busyResponse.Code != http.StatusServiceUnavailable || busyResponse.Header().Get("Retry-After") != "1" {
					t.Fatalf("busy status=%d retry=%q", busyResponse.Code, busyResponse.Header().Get("Retry-After"))
				}
				return
			}
			if response.Code == http.StatusAccepted || len(service.queue) != 0 {
				t.Fatalf("status=%d queue=%d", response.Code, len(service.queue))
			}
		})
	}
}

func TestAuthenticatedProofIsClaimedOnceBeforeAdmission(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	now := time.Now().UTC()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "018f47d2-7f00-7abc-8def-1234567890ab"
	signature, err := telemetry.SignProof(key, timestamp, nonce, []byte(telemetry.ProofRequestBody))
	if err != nil {
		t.Fatal(err)
	}
	service := &clientTelemetryService{
		queue: make(chan admittedClientEvent, 2), activeProofKey: key,
		nonces: &recordingNonceClaimer{}, clock: clock.Fixed{Instant: now},
	}
	handler := clientTelemetryBoundary(clock.Fixed{Instant: now}, service.Handle(nil))
	request := func() *http.Request {
		value := httptest.NewRequest(http.MethodPost, "/telemetry/v1", bytes.NewBufferString(telemetry.ProofRequestBody))
		value.Header.Set("Content-Type", "application/json")
		value.Header.Set(proofTimestampHeader, timestamp)
		value.Header.Set(proofNonceHeader, nonce)
		value.Header.Set(proofSignatureHeader, signature)
		return value
	}
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request())
	if first.Code != http.StatusAccepted || len(service.queue) != 1 {
		t.Fatalf("first status=%d queue=%d body=%s", first.Code, len(service.queue), first.Body.String())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request())
	if second.Code != http.StatusNotFound || len(service.queue) != 1 {
		t.Fatalf("replay status=%d queue=%d body=%s", second.Code, len(service.queue), second.Body.String())
	}
}
