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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fyannk/objectstoreviewer/internal/evidenceapi"
)

func TestRunProbeLoadsOnlyEvidenceTokenConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	encoded := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x62}, 32))
	if err := os.WriteFile(path, []byte(encoded+"\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o440); err != nil {
		t.Fatal(err)
	}
	called := false
	err := runProbe(func(key string) string {
		if key == "EVIDENCE_TOKEN_FILE" {
			return path
		}
		return "invalid-provider-configuration-must-be-ignored"
	}, func(ctx context.Context, token evidenceapi.Token) error {
		called = true
		if ctx == nil || token.String() != "[REDACTED]" {
			t.Fatal("probe received invalid bounded inputs")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("runProbe() = %v, called = %t", err, called)
	}
}

func TestRunProbeCollapsesConfigurationAndTransportDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "path-canary")
	if err := os.WriteFile(path, []byte("malformed-token-canary"), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o440); err != nil {
		t.Fatal(err)
	}
	called := false
	err := runProbe(func(string) string { return path }, func(context.Context, evidenceapi.Token) error {
		called = true
		return errors.New("transport-canary")
	})
	if !errors.Is(err, evidenceapi.ErrProbeFailed) || called || strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "canary") {
		t.Fatalf("malformed-token probe = %v, called = %t", err, called)
	}

	encoded := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x63}, 32))
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(encoded), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o440); err != nil {
		t.Fatal(err)
	}
	err = runProbe(func(string) string { return path }, func(context.Context, evidenceapi.Token) error {
		return errors.New("transport-canary")
	})
	if !errors.Is(err, evidenceapi.ErrProbeFailed) || strings.Contains(err.Error(), "transport-canary") {
		t.Fatalf("transport probe = %v", err)
	}
}
