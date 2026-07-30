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

// Package application assembles the provider-neutral runtime and background
// inventory lifecycle.
package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	evidencev1alpha1 "github.com/fyannk/pgObjectStoreViewer/api/evidence/v1alpha1"
	"github.com/fyannk/pgObjectStoreViewer/internal/config"
	"github.com/fyannk/pgObjectStoreViewer/internal/evidenceapi"
	"github.com/fyannk/pgObjectStoreViewer/internal/fault"
	"github.com/fyannk/pgObjectStoreViewer/internal/formats"
	"github.com/fyannk/pgObjectStoreViewer/internal/formats/barmancloud"
	"github.com/fyannk/pgObjectStoreViewer/internal/inventory"
	azurestore "github.com/fyannk/pgObjectStoreViewer/internal/provider/azure"
	gcsstore "github.com/fyannk/pgObjectStoreViewer/internal/provider/gcs"
	s3store "github.com/fyannk/pgObjectStoreViewer/internal/provider/s3"
	"github.com/fyannk/pgObjectStoreViewer/internal/readiness"
	"github.com/fyannk/pgObjectStoreViewer/internal/store"
	"github.com/fyannk/pgObjectStoreViewer/internal/web"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 15 * time.Second
	maxHeaderBytes    = 16 * 1024
)

type App struct {
	server          *http.Server
	evidenceHandler *evidenceapi.Handler
	serveEvidence   func(context.Context, *evidenceapi.Handler) error
	runtimeMode     config.RuntimeMode
	readiness       *readiness.ProbeState
	worker          interface{ Run(context.Context) }
}

type readerFactory func(context.Context, config.Config) (store.Reader, error)

func New(ctx context.Context, cfg config.Config, logger *slog.Logger, producerVersion string) (*App, error) {
	return newWithFactoryVersion(ctx, cfg, logger, defaultReaderFactory, producerVersion)
}

func newWithFactory(ctx context.Context, cfg config.Config, logger *slog.Logger, factory readerFactory) (*App, error) {
	return newWithFactoryVersion(ctx, cfg, logger, factory, "test")
}

func newWithFactoryVersion(ctx context.Context, cfg config.Config, logger *slog.Logger, factory readerFactory, producerVersion string) (*App, error) {
	if factory == nil {
		return nil, errors.New("application requires a reader factory")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	registry, err := formats.Builtins()
	if err != nil {
		return nil, err
	}
	format, err := registry.Select(string(cfg.RepositoryFormat))
	if err != nil {
		return nil, err
	}
	descriptor := format.Descriptor()
	cache, err := inventory.NewCache(inventory.Initial(descriptor))
	if err != nil {
		return nil, err
	}
	if cfg.RuntimeMode == config.RuntimePGConsoleSidecar && len(cfg.BarmanServerNames) != 1 {
		return nil, errors.New("invalid sidecar evidence configuration")
	}
	probeState := readiness.New(true, 2*cfg.CatalogRefreshInterval+cfg.StoreRequestTimeout, time.Now)
	reader, err := factory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if cfg.RuntimeMode == config.RuntimePGConsoleSidecar && reader != nil {
		reader, err = newPrefixConfinedReader(reader, cfg.BarmanServerNames[0]+"/")
		if err != nil {
			return nil, err
		}
	}
	scannerCache := inventory.SnapshotCache(cache)
	app := &App{runtimeMode: cfg.RuntimeMode, readiness: probeState, serveEvidence: evidenceapi.ServeUnix}
	if cfg.RuntimeMode == config.RuntimePGConsoleSidecar {
		engine, engineErr := newEvidenceEngine(cfg, producerVersion, cache.Load())
		if engineErr != nil {
			return nil, engineErr
		}
		token, tokenErr := evidenceapi.LoadTokenFile(cfg.EvidenceTokenFile)
		if tokenErr != nil {
			return nil, tokenErr
		}
		handler, handlerErr := evidenceapi.NewHandler(evidenceapi.HandlerOptions{Engine: engine, Readiness: probeState, Token: token, Logger: logger})
		if handlerErr != nil {
			return nil, handlerErr
		}
		app.evidenceHandler = handler
		scannerCache = &evidencePublishingCache{cache: cache, engine: engine}
	}
	var worker interface{ Run(context.Context) }
	if reader != nil {
		configuredScopes := cfg.BarmanServerNames
		if cfg.RepositoryFormat == config.FormatPGBackRest {
			configuredScopes = cfg.PGBackRestStanzas
		}
		recentLimit := min(cfg.WALPageSize, inventory.MaxRecentObjects)
		worker, err = inventory.NewScanner(inventory.ScannerOptions{
			Store: reader, Format: format, ConfiguredScopes: configuredScopes,
			Cache: scannerCache, Readiness: probeState, RefreshInterval: cfg.CatalogRefreshInterval,
			MaxObjects: cfg.MaxObjectsPerScan, PageSize: store.MaxPageObjects, AnalyzeBarmanCatalog: cfg.RepositoryFormat == config.FormatBarmanCloud,
			BarmanRecoveryOptions: barmancloud.RecoveryOptions{ExpectedRetentionPolicy: cfg.ExpectedRetentionPolicy, ExpectedMinimumRedundancy: cfg.ExpectedMinimumRedundancy},
			RecentLimit:           recentLimit, Now: time.Now, Logger: logger,
		})
		if err != nil {
			return nil, err
		}
	}
	app.worker = worker
	if cfg.RuntimeMode == config.RuntimePGConsoleSidecar {
		return app, nil
	}
	handler, err := web.New(web.Options{
		Logger: logger, Provider: string(cfg.Provider), Format: descriptor,
		Inventory: cache.Load,
		Readiness: probeState.Result, TrustedUserHeader: cfg.TrustedUserHeader,
		WALPageSize: cfg.WALPageSize,
	})
	if err != nil {
		return nil, err
	}
	app.server = &http.Server{
		Handler:           handler.Routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	return app, nil
}

type evidencePublishingCache struct {
	cache  *inventory.Cache
	engine *evidenceapi.Engine
}

func (c *evidencePublishingCache) Load() inventory.Snapshot { return c.cache.Load() }

func (c *evidencePublishingCache) Publish(snapshot inventory.Snapshot) error {
	if err := c.engine.Publish(snapshot); err != nil {
		return err
	}
	return c.cache.Publish(snapshot)
}

// prefixConfinedReader narrows a possibly broader provider credential to the
// single format-native scope selected by the sidecar contract. Keys remain
// repository-root-relative because Barman owns and validates that layout.
type prefixConfinedReader struct {
	reader store.Reader
	prefix string
}

var errStoreConfinement = storeConfinementError{}

type storeConfinementError struct{}

func (storeConfinementError) Error() string {
	return "sidecar store request escaped its configured scope"
}
func (storeConfinementError) Unwrap() error            { return store.ErrInvalidRequest }
func (storeConfinementError) Category() fault.Category { return fault.SafetyLimit }

func newPrefixConfinedReader(reader store.Reader, prefix string) (*prefixConfinedReader, error) {
	if reader == nil || prefix == "" || !strings.HasSuffix(prefix, "/") || len(prefix) > store.MaxKeyBytes {
		return nil, errors.New("invalid sidecar store confinement")
	}
	return &prefixConfinedReader{reader: reader, prefix: prefix}, nil
}

func (r *prefixConfinedReader) List(ctx context.Context, request store.ListRequest) (store.Page, error) {
	if request.Prefix == "" {
		request.Prefix = r.prefix
	} else if !strings.HasPrefix(request.Prefix, r.prefix) {
		return store.Page{}, errStoreConfinement
	}
	page, err := r.reader.List(ctx, request)
	if err != nil {
		return store.Page{}, err
	}
	for _, object := range page.Objects {
		if !strings.HasPrefix(object.Key, r.prefix) {
			return store.Page{}, errStoreConfinement
		}
	}
	return page, nil
}

func (r *prefixConfinedReader) Open(ctx context.Context, request store.OpenRequest) (io.ReadCloser, error) {
	if !strings.HasPrefix(request.Key, r.prefix) {
		return nil, errStoreConfinement
	}
	return r.reader.Open(ctx, request)
}

func (r *prefixConfinedReader) Stat(ctx context.Context, request store.StatRequest) (store.Object, error) {
	if !strings.HasPrefix(request.Key, r.prefix) {
		return store.Object{}, errStoreConfinement
	}
	object, err := r.reader.Stat(ctx, request)
	if err != nil {
		return store.Object{}, err
	}
	if !strings.HasPrefix(object.Key, r.prefix) {
		return store.Object{}, errStoreConfinement
	}
	return object, nil
}

func newEvidenceEngine(cfg config.Config, producerVersion string, initial inventory.Snapshot) (*evidenceapi.Engine, error) {
	if cfg.RuntimeMode != config.RuntimePGConsoleSidecar || cfg.Destination == nil || len(cfg.BarmanServerNames) != 1 {
		return nil, errors.New("invalid sidecar evidence configuration")
	}
	endpoint := ""
	if cfg.Endpoint != nil {
		endpoint = cfg.Endpoint.String()
	}
	var clusterName *string
	if cfg.CNPGClusterName != "" {
		value := cfg.CNPGClusterName
		clusterName = &value
	}
	return evidenceapi.NewEngine(evidenceapi.EngineOptions{
		Projection: evidenceapi.Options{
			ProducerVersion: producerVersion, ClusterNamespace: cfg.CNPGClusterNamespace,
			ClusterUID: cfg.CNPGClusterUID, ClusterName: clusterName,
			S3: evidencev1alpha1.S3FingerprintInput{
				Endpoint: endpoint, Region: cfg.Credentials.AWSRegion, Bucket: cfg.Destination.Host,
				Prefix: strings.Trim(cfg.Destination.Path, "/"), Format: string(cfg.RepositoryFormat),
				ScopeKind: "barman-server", ScopeName: cfg.BarmanServerNames[0],
			},
		},
		Initial: initial,
	})
}

// Serve blocks until the listener fails or ctx requests graceful shutdown.
func (a *App) Serve(ctx context.Context, listener net.Listener) error {
	if a.runtimeMode != config.RuntimeStandalone || a.server == nil || listener == nil {
		return errors.New("standalone application runtime is unavailable")
	}
	return a.serveRuntime(ctx, func(runtimeCtx context.Context) error {
		return a.serveHTTP(runtimeCtx, listener)
	})
}

// ServeSidecar runs the scanner and fixed private evidence socket until ctx is
// canceled. It never creates a TCP listener.
func (a *App) ServeSidecar(ctx context.Context) error {
	if a.runtimeMode != config.RuntimePGConsoleSidecar || a.evidenceHandler == nil || a.serveEvidence == nil {
		return errors.New("sidecar application runtime is unavailable")
	}
	return a.serveRuntime(ctx, func(runtimeCtx context.Context) error {
		return a.serveEvidence(runtimeCtx, a.evidenceHandler)
	})
}

func (a *App) serveRuntime(ctx context.Context, serve func(context.Context) error) error {
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	var workers sync.WaitGroup
	if a.worker != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			a.worker.Run(runtimeCtx)
		}()
	}
	defer func() {
		cancelRuntime()
		workers.Wait()
	}()
	return serve(runtimeCtx)
}

func (a *App) serveHTTP(ctx context.Context, listener net.Listener) error {
	a.server.BaseContext = func(net.Listener) context.Context { return ctx }
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- a.server.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			_ = a.server.Close()
			return err
		}
		err := <-serveErrors
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func defaultReaderFactory(ctx context.Context, cfg config.Config) (store.Reader, error) {
	switch cfg.Provider {
	case config.ProviderS3:
		return s3store.New(ctx, s3store.Options{
			Bucket: cfg.Destination.Host, Prefix: strings.Trim(cfg.Destination.Path, "/"),
			Endpoint: cfg.Endpoint, CABundle: cfg.EndpointCABundle, Region: cfg.Credentials.AWSRegion,
			AccessKeyID: cfg.Credentials.AWSAccessKeyID.Bytes(), SecretAccessKey: cfg.Credentials.AWSSecretKey.Bytes(),
			SessionToken: cfg.Credentials.AWSSessionToken.Bytes(), WebIdentityTokenFile: cfg.Credentials.AWSWebIdentityTokenFile,
			RoleARN: cfg.Credentials.AWSRoleARN, RequestTimeout: cfg.StoreRequestTimeout,
		})
	case config.ProviderAzure:
		return azurestore.New(ctx, azurestore.Options{Container: cfg.Destination.Host, Prefix: strings.Trim(cfg.Destination.Path, "/"), Account: cfg.Credentials.AzureAccount, ConnectionString: string(cfg.Credentials.AzureConnectionString.Bytes()), AccountKey: cfg.Credentials.AzureAccountKey.Bytes(), SASToken: cfg.Credentials.AzureSASToken.Bytes(), RequestTimeout: cfg.StoreRequestTimeout})
	case config.ProviderGCS:
		return gcsstore.New(ctx, gcsstore.Options{Bucket: cfg.Destination.Host, Prefix: strings.Trim(cfg.Destination.Path, "/"), CredentialsJSON: cfg.Credentials.GoogleCredentialsJSON.Bytes(), RequestTimeout: cfg.StoreRequestTimeout})
	default:
		return nil, errors.New("unsupported provider")
	}
}

// ReadinessState is the Slice 1 seam for a lightweight store probe.
func (a *App) ReadinessState() *readiness.ProbeState { return a.readiness }
