package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"go.opentelemetry.io/otel/trace"
)

const (
	clientTelemetryBodyLimit = 16 << 10
	clientTelemetryQueueSize = 1024
	clientTelemetryWorkers   = 4
	clientTelemetryDrain     = 5 * time.Second
	proofTimestampHeader     = "X-Accountable-Proof-Timestamp"
	proofNonceHeader         = "X-Accountable-Proof-Nonce"
	proofSignatureHeader     = "X-Accountable-Proof-Signature"
)

type admittedClientEvent struct {
	envelope telemetry.ClientEnvelope
	identity telemetry.WorkIdentity
	proof    bool
}

type proofNonceClaimer interface {
	ClaimProofNonce(context.Context, string, time.Time) (bool, error)
}

type clientTelemetryService struct {
	active          telemetry.ClientIdentityTuple
	previous        *telemetry.ClientIdentityTuple
	recorder        *telemetry.Recorder
	logger          *telemetry.SafeLogger
	queue           chan admittedClientEvent
	workers         sync.WaitGroup
	close           sync.Once
	admission       sync.RWMutex
	stopping        bool
	activeProofKey  string
	standbyProofKey string
	nonces          proofNonceClaimer
	clock           clock.Clock
}

func newClientTelemetryService(
	active telemetry.ClientIdentityTuple,
	previous *telemetry.ClientIdentityTuple,
	release telemetry.RuntimeRelease,
	recorder *telemetry.Recorder,
	logger *telemetry.SafeLogger,
	activeProofKey string,
	standbyProofKey string,
	nonces proofNonceClaimer,
	processClock clock.Clock,
) (*clientTelemetryService, error) {
	if active.ReleaseID != release.ReleaseID || active.ArtifactDigest != release.WebAssetDigest {
		return nil, telemetry.ErrInvalidContract
	}
	if previous != nil {
		if err := telemetry.ValidateClientIdentityTuple(*previous); err != nil || *previous == active {
			return nil, telemetry.ErrInvalidContract
		}
	}
	if _, err := telemetry.DecodeProofKey(activeProofKey); err != nil || nonces == nil || processClock == nil {
		return nil, telemetry.ErrInvalidContract
	}
	if standbyProofKey != "" {
		if _, err := telemetry.DecodeProofKey(standbyProofKey); err != nil {
			return nil, telemetry.ErrInvalidContract
		}
	}
	service := &clientTelemetryService{
		active: active, previous: previous, recorder: recorder, logger: logger,
		queue:          make(chan admittedClientEvent, clientTelemetryQueueSize),
		activeProofKey: activeProofKey, standbyProofKey: standbyProofKey, nonces: nonces,
		clock: processClock,
	}
	for range clientTelemetryWorkers {
		service.workers.Add(1)
		go service.work()
	}
	return service, nil
}

func (service *clientTelemetryService) Handle(allowedOrigins []string) http.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		if request.Header.Get("Content-Encoding") != "" {
			writeClientTelemetryError(response, http.StatusBadRequest, "invalid_telemetry")
			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeClientTelemetryError(response, http.StatusUnsupportedMediaType, "invalid_telemetry")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(request.Body, clientTelemetryBodyLimit+1))
		if err != nil || len(raw) == 0 || len(raw) > clientTelemetryBodyLimit {
			writeClientTelemetryError(response, http.StatusBadRequest, "invalid_telemetry")
			return
		}
		proofHeaders := telemetry.ProofHeaders{
			Timestamp: request.Header.Get(proofTimestampHeader),
			Nonce:     request.Header.Get(proofNonceHeader),
			Signature: request.Header.Get(proofSignatureHeader),
		}
		proofMode := proofHeaders.Timestamp != "" || proofHeaders.Nonce != "" || proofHeaders.Signature != ""
		if proofMode {
			if telemetry.VerifyProof(telemetry.ProofVerification{
				Headers: proofHeaders, Body: raw, ServerTime: service.clock.Now(),
				ActiveKey: service.activeProofKey, StandbyKey: service.standbyProofKey,
			}) != nil {
				writeProofNotFound(response, request.Context())
				return
			}
			claimed, err := service.nonces.ClaimProofNonce(request.Context(), proofHeaders.Nonce, service.clock.Now().Add(2*time.Minute))
			if err != nil {
				response.Header().Set("Retry-After", "1")
				writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_unavailable")
				return
			}
			if !claimed {
				writeProofNotFound(response, request.Context())
				return
			}
			identity, _ := telemetry.WorkIdentityFromContext(request.Context())
			identity.EventID, err = telemetry.NewWorkID()
			if err != nil {
				writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_unavailable")
				return
			}
			service.admission.RLock()
			if service.stopping {
				service.admission.RUnlock()
				response.Header().Set("Retry-After", "1")
				writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_busy")
				return
			}
			select {
			case service.queue <- admittedClientEvent{identity: identity, proof: true}:
				response.WriteHeader(http.StatusAccepted)
			default:
				response.Header().Set("Retry-After", "1")
				writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_busy")
			}
			service.admission.RUnlock()
			return
		}
		if _, accepted := allowed[request.Header.Get("Origin")]; !accepted {
			writeClientTelemetryError(response, http.StatusNotFound, "not_found")
			return
		}
		envelope, err := telemetry.ParseClientEnvelope(raw)
		if err != nil || envelope.CorrelationID != request.Header.Get(telemetry.HeaderCorrelationID) ||
			telemetry.ValidateClientAdmission(envelope, telemetry.ClientAdmissionPolicy{
				Now:              request.Context().Value(admissionTimeKey{}).(time.Time),
				ActiveIdentity:   service.active,
				PreviousIdentity: service.previous,
			}) != nil {
			writeClientTelemetryError(response, http.StatusBadRequest, "invalid_telemetry")
			return
		}
		eventID, err := telemetry.NewWorkID()
		if err != nil {
			writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_unavailable")
			return
		}
		identity, _ := telemetry.WorkIdentityFromContext(request.Context())
		identity.EventID = eventID
		service.admission.RLock()
		if service.stopping {
			service.admission.RUnlock()
			response.Header().Set("Retry-After", "1")
			writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_busy")
			return
		}
		select {
		case service.queue <- admittedClientEvent{envelope: envelope, identity: identity}:
			response.WriteHeader(http.StatusAccepted)
		default:
			response.Header().Set("Retry-After", "1")
			writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_busy")
		}
		service.admission.RUnlock()
	}
}

type admissionTimeKey struct{}

func clientTelemetryBoundary(processClock clock.Clock, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		identity, err := telemetry.StartRequestIdentity(request.Header.Get(telemetry.HeaderCorrelationID))
		if err != nil {
			writeClientTelemetryError(response, http.StatusServiceUnavailable, "telemetry_unavailable")
			return
		}
		response.Header().Set(telemetry.HeaderRequestID, identity.RequestID)
		response.Header().Set(telemetry.HeaderCorrelationID, identity.CorrelationID)
		ctx := telemetry.ContextWithWorkIdentity(request.Context(), identity)
		ctx = context.WithValue(ctx, admissionTimeKey{}, processClock.Now())
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (service *clientTelemetryService) work() {
	defer service.workers.Done()
	for event := range service.queue {
		service.process(event)
	}
}

func (service *clientTelemetryService) process(event admittedClientEvent) {
	ctx := telemetry.ContextWithWorkIdentity(context.Background(), event.identity)
	defer func() {
		if recover() != nil {
			service.drop(ctx, "worker_failure", 1)
		}
	}()
	if event.proof {
		attributes, _ := telemetry.SpanAttributes(event.identity, map[string]string{"proof_type": telemetry.ProofType})
		ctx, span := service.recorder.StartInternalSpan(ctx, "observability.proof", trace.SpanKindInternal, attributes)
		service.recorder.ProofProcessed(ctx)
		_ = service.logger.Emit(ctx, slog.LevelInfo, "observability.proof", map[string]string{"proof_type": telemetry.ProofType})
		span.End()
		return
	}
	attributes, err := telemetry.SpanAttributes(event.identity, map[string]string{
		"event_name":  event.envelope.EventName,
		"route_class": event.envelope.RouteClass,
	})
	if err != nil {
		service.drop(ctx, "invalid_internal_state", 1)
		return
	}
	ctx, span := service.recorder.StartInternalSpan(ctx, "client.event", trace.SpanKindConsumer, attributes)
	defer span.End()
	if err := service.recorder.ClientEventProcessed(ctx, event.envelope); err != nil {
		service.drop(ctx, "worker_failure", 1)
		return
	}
	_ = service.logger.Emit(ctx, slog.LevelInfo, "client.event", map[string]string{
		"event_name":  event.envelope.EventName,
		"route_class": event.envelope.RouteClass,
	})
}

func (service *clientTelemetryService) drop(ctx context.Context, reason string, count int) {
	_ = service.logger.EmitClientDrop(ctx, reason, int64(count))
}

func (service *clientTelemetryService) Shutdown(ctx context.Context) error {
	service.close.Do(func() {
		service.admission.Lock()
		service.stopping = true
		close(service.queue)
		service.admission.Unlock()
	})
	done := make(chan struct{})
	go func() {
		service.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func writeClientTelemetryError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"code": code})
}

func writeProofNotFound(response http.ResponseWriter, ctx context.Context) {
	identity, _ := telemetry.WorkIdentityFromContext(ctx)
	problemID, err := telemetry.NewWorkID()
	if err != nil {
		writeClientTelemetryError(response, http.StatusNotFound, "not_found")
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusNotFound)
	_, _ = response.Write([]byte(`{"code":"not_found","message_key":"errors.notFound","problem_id":"` + problemID + `","request_id":"` + identity.RequestID + `","correlation_id":"` + identity.CorrelationID + `"}`))
}
