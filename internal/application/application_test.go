// Copyright 2026 The ObjectStoreViewer Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	evidencev1alpha1 "github.com/fyannk/pgObjectStoreViewer/api/evidence/v1alpha1"
	"github.com/fyannk/pgObjectStoreViewer/internal/config"
	"github.com/fyannk/pgObjectStoreViewer/internal/evidenceapi"
	"github.com/fyannk/pgObjectStoreViewer/internal/fault"
	"github.com/fyannk/pgObjectStoreViewer/internal/inventory"
	"github.com/fyannk/pgObjectStoreViewer/internal/store"
	"github.com/fyannk/pgObjectStoreViewer/internal/store/storetest"
)

func fakeReaderFactory(context.Context, config.Config) (store.Reader, error) {
	return &storetest.Fake{}, nil
}

func TestServeStopsGracefullyWhenContextIsCanceled(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"REPOSITORY_FORMAT": "barman-cloud",
			"PROVIDER":          "s3",
			"DESTINATION_PATH":  "s3://backups/prefix",
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := newWithFactory(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fakeReaderFactory)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, listener) }()

	response, err := http.Get("http://" + listener.Addr().String() + "/healthz")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = response.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not stop after cancellation")
	}
}

func TestApplicationNeverRendersOrLogsConfiguredSecrets(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	accessCanary := "access-canary-never-cross-boundary"
	secretCanary := "secret-canary-never-cross-boundary"
	accessPath := filepath.Join(directory, "access")
	secretPath := filepath.Join(directory, "secret")
	if err := os.WriteFile(accessPath, []byte(accessCanary), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte(secretCanary), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"REPOSITORY_FORMAT":          "barman-cloud",
			"PROVIDER":                   "s3",
			"DESTINATION_PATH":           "s3://topology-canary/private-prefix",
			"AWS_ACCESS_KEY_ID_FILE":     accessPath,
			"AWS_SECRET_ACCESS_KEY_FILE": secretPath,
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	app, err := newWithFactory(context.Background(), cfg, slog.New(slog.NewJSONHandler(logs, nil)), fakeReaderFactory)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, listener) }()

	response, err := http.Get("http://" + listener.Addr().String() + "/")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	output := string(body) + logs.String()
	for _, forbidden := range []string{accessCanary, secretCanary, accessPath, secretPath, "topology-canary", "private-prefix"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("application boundary disclosed %q", forbidden)
		}
	}
}

func TestServeDrainsInflightRequestOnCancellation(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"REPOSITORY_FORMAT": "barman-cloud", "PROVIDER": "s3", "DESTINATION_PATH": "s3://backups/prefix",
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := newWithFactory(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fakeReaderFactory)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	app.server.Handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writer.WriteHeader(http.StatusNoContent)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- app.Serve(ctx, listener) }()
	requestDone := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/")
		if requestErr == nil {
			_ = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	<-started
	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("Serve returned before the in-flight request drained: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatalf("in-flight request failed during drain: %v", err)
	}
	if err := <-serveDone; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestBrowserUsesCompletedCacheAndPerformsNoStoreCalls(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"REPOSITORY_FORMAT": "barman-cloud", "PROVIDER": "s3", "DESTINATION_PATH": "s3://backups/prefix",
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	var listCalls atomic.Int64
	factory := func(context.Context, config.Config) (store.Reader, error) {
		return &storetest.Fake{ListFunc: func(context.Context, store.ListRequest) (store.Page, error) {
			listCalls.Add(1)
			return store.Page{Objects: []store.Object{{Key: "alpha/base/20260727/backup.info", Size: 21}}}, nil
		}}, nil
	}
	app, err := newWithFactory(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), factory)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, listener) }()
	baseURL := "http://" + listener.Addr().String()
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := http.Get(baseURL + "/readyz")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("background inventory never became ready")
		}
		time.Sleep(time.Millisecond)
	}
	for {
		response, requestErr := http.Get(baseURL + "/")
		if requestErr == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr == nil && strings.Contains(string(body), "alpha") {
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("completed inventory never reached the cache")
		}
		time.Sleep(time.Millisecond)
	}
	before := listCalls.Load()
	for _, path := range []string{"/", "/", "/wals", "/wals?class=gap"} {
		response, requestErr := http.Get(baseURL + path)
		if requestErr != nil {
			cancel()
			t.Fatal(requestErr)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			cancel()
			t.Fatal(readErr)
		}
		if !strings.Contains(string(body), "<dd>1</dd>") || (path == "/" && !strings.Contains(string(body), "alpha")) || (path != "/" && !strings.Contains(string(body), "Barman WAL evidence")) {
			cancel()
			t.Fatalf("cached inventory missing: %s", body)
		}
	}
	if after := listCalls.Load(); after != before {
		cancel()
		t.Fatalf("browser requests performed store calls: before=%d after=%d", before, after)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSidecarRuntimePublishesScannerEvidenceWithoutRequestStoreCalls(t *testing.T) {
	directory := t.TempDir()
	accessPath := filepath.Join(directory, "access")
	secretPath := filepath.Join(directory, "secret")
	tokenPath := filepath.Join(directory, "evidence-token")
	tokenValue := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x71}, 32))
	for path, fixture := range map[string]struct {
		value string
		mode  os.FileMode
	}{
		accessPath: {value: "synthetic-access", mode: 0o600},
		secretPath: {value: "synthetic-secret", mode: 0o600},
		tokenPath:  {value: tokenValue + "\n", mode: 0o440},
	} {
		if err := os.WriteFile(path, []byte(fixture.value), fixture.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, fixture.mode); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"RUNTIME_MODE": "pgconsole-sidecar", "REPOSITORY_FORMAT": "barman-cloud", "PROVIDER": "s3",
			"DESTINATION_PATH": "s3://synthetic-bucket/cluster", "BARMAN_SERVER_NAMES": "orders",
			"EVIDENCE_TOKEN_FILE": tokenPath, "CNPG_CLUSTER_NAMESPACE": "database-team",
			"CNPG_CLUSTER_UID": "2f12b7d1-7e8d-4c37-a68f-233efc5f3191", "CNPG_CLUSTER_NAME": "orders",
			"STORE_CREDENTIAL_MODE": "static-files", "AWS_ACCESS_KEY_ID_FILE": accessPath,
			"AWS_SECRET_ACCESS_KEY_FILE": secretPath, "AWS_REGION": "eu-west-1",
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	var listCalls atomic.Int64
	var badListPrefix atomic.Bool
	var failList atomic.Bool
	factory := func(context.Context, config.Config) (store.Reader, error) {
		return &storetest.Fake{ListFunc: func(_ context.Context, request store.ListRequest) (store.Page, error) {
			if request.Prefix != "orders/" {
				badListPrefix.Store(true)
			}
			listCalls.Add(1)
			if failList.Load() {
				return store.Page{}, context.DeadlineExceeded
			}
			return store.Page{}, nil
		}}, nil
	}
	app, err := newWithFactoryVersion(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), factory, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if app.server != nil || app.evidenceHandler == nil {
		t.Fatalf("sidecar surfaces = server:%v evidence:%v", app.server != nil, app.evidenceHandler != nil)
	}
	handlerReady := make(chan *evidenceapi.Handler, 1)
	app.serveEvidence = func(ctx context.Context, handler *evidenceapi.Handler) error {
		handlerReady <- handler
		<-ctx.Done()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.ServeSidecar(ctx) }()
	handler := <-handlerReady

	var snapshot evidencev1alpha1.RepositoryEvidenceSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for {
		response := sidecarRequest(handler, tokenValue, "/api/v1alpha1/snapshot")
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &snapshot) == nil && snapshot.Revision > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("scanner publication did not reach engine: %s", response.Body.String())
		}
		time.Sleep(time.Millisecond)
	}
	if snapshot.Producer.Version != "1.2.3" || snapshot.Identity.Cluster.Namespace != "database-team" || snapshot.Identity.Cluster.UID != cfg.CNPGClusterUID || snapshot.Identity.Repository.Scope.Name != "orders" {
		t.Fatalf("sidecar identity = %#v", snapshot)
	}
	if badListPrefix.Load() {
		t.Fatal("sidecar scanner listed outside the configured Barman server prefix")
	}
	before := listCalls.Load()
	for _, path := range []string{
		"/healthz", "/readyz", "/api/v1alpha1/snapshot",
		"/api/v1alpha1/backups?revision=" + strconv.FormatUint(snapshot.Revision, 10),
	} {
		response := sidecarRequest(handler, tokenValue, path)
		if response.Code != http.StatusOK {
			cancel()
			t.Fatalf("%s status = %d: %s", path, response.Code, response.Body.String())
		}
	}
	if after := listCalls.Load(); after != before {
		cancel()
		t.Fatalf("evidence requests performed store calls: before=%d after=%d", before, after)
	}
	scanner, ok := app.worker.(*inventory.Scanner)
	if !ok {
		cancel()
		t.Fatalf("sidecar worker = %T", app.worker)
	}
	failList.Store(true)
	if err := scanner.Refresh(context.Background()); err == nil {
		cancel()
		t.Fatal("failed refresh error = nil")
	}
	afterFailure := listCalls.Load()
	response := sidecarRequest(handler, tokenValue, "/api/v1alpha1/snapshot")
	var retained evidencev1alpha1.RepositoryEvidenceSnapshot
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &retained) != nil {
		cancel()
		t.Fatalf("retained snapshot response = %d: %s", response.Code, response.Body.String())
	}
	if retained.Revision <= snapshot.Revision || retained.EvidenceGeneration != snapshot.EvidenceGeneration || !retained.Stale || retained.State != evidencev1alpha1.Unknown {
		cancel()
		t.Fatalf("retained failed-refresh evidence = %#v", retained)
	}
	if listCalls.Load() != afterFailure {
		cancel()
		t.Fatal("failed-refresh evidence request performed a store call")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSidecarReaderConfinesEveryOperationToConfiguredScope(t *testing.T) {
	var listCalls, openCalls, statCalls atomic.Int64
	reader, err := newPrefixConfinedReader(&storetest.Fake{
		ListFunc: func(_ context.Context, request store.ListRequest) (store.Page, error) {
			listCalls.Add(1)
			if request.Prefix != "orders/" {
				t.Fatalf("forwarded list prefix = %q", request.Prefix)
			}
			return store.Page{Objects: []store.Object{{Key: "orders/base/backup/data.tar"}}}, nil
		},
		OpenFunc: func(_ context.Context, request store.OpenRequest) (io.ReadCloser, error) {
			openCalls.Add(1)
			return io.NopCloser(strings.NewReader(request.Key)), nil
		},
		StatFunc: func(_ context.Context, key string) (store.Object, error) {
			statCalls.Add(1)
			return store.Object{Key: key}, nil
		},
	}, "orders/")
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.List(context.Background(), store.ListRequest{Limit: 1})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != "orders/base/backup/data.tar" {
		t.Fatalf("confined List() = %#v, %v", page, err)
	}
	body, err := reader.Open(context.Background(), store.OpenRequest{Key: "orders/base/backup/backup.info", MaxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	_ = body.Close()
	if _, err := reader.Stat(context.Background(), store.StatRequest{Key: "orders/base/backup/data.tar"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.List(context.Background(), store.ListRequest{Prefix: "archive/", Limit: 1}); !errors.Is(err, store.ErrInvalidRequest) {
		t.Fatalf("outside List() error = %v", err)
	}
	if _, err := reader.Open(context.Background(), store.OpenRequest{Key: "archive/base/backup.info", MaxBytes: 64}); !errors.Is(err, store.ErrInvalidRequest) {
		t.Fatalf("outside Open() error = %v", err)
	}
	if _, err := reader.Stat(context.Background(), store.StatRequest{Key: "archive/base/data.tar"}); !errors.Is(err, store.ErrInvalidRequest) {
		t.Fatalf("outside Stat() error = %v", err)
	}
	if listCalls.Load() != 1 || openCalls.Load() != 1 || statCalls.Load() != 1 {
		t.Fatalf("provider calls list=%d open=%d stat=%d", listCalls.Load(), openCalls.Load(), statCalls.Load())
	}

	malicious, err := newPrefixConfinedReader(&storetest.Fake{ListFunc: func(context.Context, store.ListRequest) (store.Page, error) {
		return store.Page{Objects: []store.Object{{Key: "archive/base/backup.info"}}}, nil
	}}, "orders/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := malicious.List(context.Background(), store.ListRequest{Limit: 1}); !errors.Is(err, store.ErrInvalidRequest) || fault.Categorize(err) != fault.SafetyLimit {
		t.Fatalf("out-of-prefix provider response error = %v", err)
	}
}

func sidecarRequest(handler http.Handler, token, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
