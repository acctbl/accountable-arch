package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acctbl/accountable/internal/bootstrap"
	"github.com/acctbl/accountable/internal/platform/database"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

type stubDependencies struct{ unavailable atomic.Int32 }

func (d *stubDependencies) Ping(context.Context) error {
	if d.unavailable.Load() != 0 {
		return database.ErrDatabaseUnavailable
	}
	return nil
}
func (d *stubDependencies) Close() {}

func TestBootstrapFailureNeverStartsServing(t *testing.T) {
	t.Parallel()

	want := errors.New("bootstrap refused")
	serveCalled := false
	err := bootstrapAndServe(
		context.Background(),
		config{},
		testRelease(),
		&runtimeSignals{},
		func(context.Context, bootstrap.Config) (ownedDependencySet, error) { return nil, want },
		func(context.Context, config, telemetry.RuntimeRelease, *runtimeSignals, dependencySet) error {
			serveCalled = true
			return nil
		},
	)
	if !errors.Is(err, want) {
		t.Fatalf("bootstrapAndServe error = %v", err)
	}
	if serveCalled {
		t.Fatal("listener-serving path ran after bootstrap refusal")
	}
}

func TestRunningAPIDropsAndRecoversReadinessWithoutExiting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve API address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release API address: %v", err)
	}

	dependencies := &stubDependencies{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveWithDependencies(ctx, config{
			Addr:              address,
			UnaryRPCDeadline:  time.Second,
			StreamRPCDeadline: time.Second,
			Foundation: bootstrap.Config{Database: database.Config{
				HealthCheckInterval: 5 * time.Millisecond,
				ConnectTimeout:      50 * time.Millisecond,
			}, CheckTimeout: 50 * time.Millisecond, ReadinessProbeInterval: 5 * time.Millisecond},
		}, testRelease(), &runtimeSignals{}, dependencies)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("API did not stop after cancellation")
		}
	})

	assertReadinessStatus(t, address, http.StatusOK)
	for _, capability := range []int32{1, 2, 3} {
		dependencies.unavailable.Store(capability)
		assertReadinessStatus(t, address, http.StatusServiceUnavailable)
		select {
		case err := <-done:
			t.Fatalf("API exited after recoverable capability failure %d: %v", capability, err)
		default:
		}
		dependencies.unavailable.Store(0)
		assertReadinessStatus(t, address, http.StatusOK)
	}
}

func assertReadinessStatus(t *testing.T, address string, want int) {
	t.Helper()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + address + "/_health/ready")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("/_health/ready did not reach status %d", want)
}

func testRelease() telemetry.RuntimeRelease {
	return telemetry.RuntimeRelease{
		SchemaVersion:         1,
		ReleaseID:             "018f47d2-7f00-7abc-8def-1234567890ab",
		ReleaseManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ServiceVersion:        "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		WebAssetDigest:        "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
}
