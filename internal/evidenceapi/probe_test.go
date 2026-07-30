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

package evidenceapi

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeHealthUsesOnlyAuthenticatedFixedLiveness(t *testing.T) {
	directory := newSocketDirectory(t)
	path := filepath.Join(directory, "evidence.sock")
	listener, err := listenUnixAt(path)
	if err != nil {
		t.Fatal(err)
	}
	handler, token := newTestHandler(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveUnixListener(ctx, listener, handler) }()

	if err := probeHealthAt(context.Background(), token, path); err != nil {
		t.Fatal(err)
	}
	if err := probeHealthAt(context.Background(), testToken(t, 0x7f), path); !errors.Is(err, ErrProbeFailed) {
		t.Fatalf("wrong-token error = %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProbeHealthBoundsFailuresWithoutDetails(t *testing.T) {
	token := testToken(t, 0x61)
	path := filepath.Join(t.TempDir(), "path-canary.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := probeHealthAt(ctx, token, path)
	if !errors.Is(err, ErrProbeFailed) || time.Since(started) > time.Second {
		t.Fatalf("bounded probe error = %v", err)
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), string(token.encoded[:])) {
		t.Fatal("probe error disclosed channel details")
	}
}
