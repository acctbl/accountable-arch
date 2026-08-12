package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/secret"
)

const (
	grafanaTimeout      = 20 * time.Second
	maxGrafanaBodyBytes = 1 << 20

	dataSourceQueryPath = "/api/ds/query"
	queryWindowMinutes  = 15
)

// grafanaClient reads one Grafana stack. It never logs a response, because a
// usage or query payload can carry organisation and user identity.
type grafanaClient struct {
	baseURL string
	token   string
	clock   clock.Clock
	client  *http.Client

	uidMutex     sync.Mutex
	dataSourceID map[string]string
}

func newGrafanaClient(ctx context.Context, settings deployment) (*grafanaClient, error) {
	token, err := resolveGrafanaToken(ctx, settings)
	if err != nil {
		return nil, err
	}
	return &grafanaClient{
		baseURL:      settings.GrafanaBaseURL,
		token:        token,
		clock:        clock.System{},
		client:       &http.Client{Timeout: grafanaTimeout},
		dataSourceID: make(map[string]string),
	}, nil
}

func (grafana *grafanaClient) send(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	base, err := url.Parse(grafana.baseURL)
	if err != nil {
		return nil, err
	}
	reference, err := url.Parse(path)
	if err != nil || reference.IsAbs() || !strings.HasPrefix(reference.Path, "/") {
		return nil, errors.New("the Grafana request path is invalid")
	}
	endpoint := base.ResolveReference(reference).String()
	requestContext, cancel := context.WithTimeout(ctx, grafanaTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(requestContext, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+grafana.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := grafana.client.Do(request)
	if err != nil {
		return nil, errors.New("the request did not reach Grafana")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the Grafana request returned HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxGrafanaBodyBytes))
}

// dataSourceUID resolves a data-source name to its stack-generated UID. Naming
// the data source keeps configuration readable; the UID is generated per stack.
func (grafana *grafanaClient) dataSourceUID(ctx context.Context, name string) (string, error) {
	grafana.uidMutex.Lock()
	defer grafana.uidMutex.Unlock()
	if uid, known := grafana.dataSourceID[name]; known {
		return uid, nil
	}
	raw, err := grafana.send(ctx, http.MethodGet, "/api/datasources/name/"+url.PathEscape(name), nil)
	if err != nil {
		return "", err
	}
	var source struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(raw, &source); err != nil || source.UID == "" {
		return "", errors.New("the data source has no UID")
	}
	grafana.dataSourceID[name] = source.UID
	return source.UID, nil
}

// query runs one data-source query over the window the producers share.
func (grafana *grafanaClient) query(ctx context.Context, target map[string]any) ([]byte, error) {
	now := grafana.clock.Now()
	body, err := json.Marshal(map[string]any{
		"from":    fmt.Sprintf("%d", now.Add(-queryWindowMinutes*time.Minute).UnixMilli()),
		"to":      fmt.Sprintf("%d", now.UnixMilli()),
		"queries": []map[string]any{target},
	})
	if err != nil {
		return nil, err
	}
	return grafana.send(ctx, http.MethodPost, dataSourceQueryPath, body)
}

func resolveGrafanaToken(ctx context.Context, settings deployment) (string, error) {
	path, ref := splitParameter(settings.TokenParameter)
	parsedRef, err := secret.ParseRef(ref)
	if err != nil {
		return "", err
	}
	source, err := secret.NewSource(ctx, secret.Config{
		Provider:      secret.ProviderParameterStore,
		AWSRegion:     settings.Region,
		ParameterPath: path,
	})
	if err != nil {
		return "", err
	}
	resolved, err := source.ResolveBatch(ctx, []secret.Ref{parsedRef})
	if err != nil {
		return "", err
	}
	value, ok := resolved[parsedRef]
	if !ok || len(value.Bytes()) == 0 {
		return "", errors.New("the Grafana token is absent")
	}
	return string(value.Bytes()), nil
}

func splitParameter(parameter string) (path string, ref string) {
	separator := strings.LastIndex(parameter, "/")
	if separator < 0 {
		return "", parameter
	}
	return parameter[:separator], parameter[separator+1:]
}
