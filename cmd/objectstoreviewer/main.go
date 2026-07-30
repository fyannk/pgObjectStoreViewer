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

package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/fyannk/pgObjectStoreViewer/internal/application"
	"github.com/fyannk/pgObjectStoreViewer/internal/config"
	"github.com/fyannk/pgObjectStoreViewer/internal/evidenceapi"
)

var version = "development"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := dispatch(os.Args[1:], logger); err != nil {
		attributes := []any{slog.String("category", errorCategory(err))}
		if config.IsError(err) {
			attributes = append(attributes, slog.String("detail", err.Error()))
		}
		logger.Error("application stopped", attributes...)
		os.Exit(1)
	}
}

func dispatch(arguments []string, logger *slog.Logger) error {
	if len(arguments) == 0 {
		return run(logger)
	}
	if len(arguments) == 1 && arguments[0] == "probe" {
		return runProbe(os.Getenv, evidenceapi.ProbeHealth)
	}
	return errors.New("unsupported command")
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.LoadOS()
	if err != nil {
		return err
	}
	app, err := application.New(ctx, cfg, logger, version)
	if err != nil {
		return err
	}
	if cfg.RuntimeMode == config.RuntimePGConsoleSidecar {
		logger.Info("sidecar application starting", slog.String("runtime_mode", string(cfg.RuntimeMode)))
		return app.ServeSidecar(ctx)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	logger.Info("application listening",
		slog.String("listen_addr", cfg.ListenAddr),
		slog.String("provider", string(cfg.Provider)),
		slog.String("repository_format", string(cfg.RepositoryFormat)),
	)
	return app.Serve(ctx, listener)
}

func runProbe(getenv func(string) string, probe func(context.Context, evidenceapi.Token) error) error {
	if getenv == nil || probe == nil {
		return evidenceapi.ErrProbeFailed
	}
	token, err := evidenceapi.LoadTokenFile(getenv("EVIDENCE_TOKEN_FILE"))
	if err != nil {
		return evidenceapi.ErrProbeFailed
	}
	if err := probe(context.Background(), token); err != nil {
		return evidenceapi.ErrProbeFailed
	}
	return nil
}

func errorCategory(err error) string {
	if config.IsError(err) {
		return "invalid_configuration"
	}
	return "runtime"
}
