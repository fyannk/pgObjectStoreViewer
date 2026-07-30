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
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	evidencev1alpha1 "github.com/fyannk/pgObjectStoreViewer/api/evidence/v1alpha1"
)

func TestListenUnixCreatesRestrictedSocketAndRemovesStaleSocket(t *testing.T) {
	directory := newSocketDirectory(t)
	path := filepath.Join(directory, "evidence.sock")

	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stale socket = %#v, %v", info, err)
	}

	listener, err := listenUnixAt(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode = %v, %v", info.Mode(), err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remained after close: %v", err)
	}
}

func TestListenUnixRejectsUnsafePathObjectsAndDirectories(t *testing.T) {
	t.Run("active socket", func(t *testing.T) {
		directory := newSocketDirectory(t)
		path := filepath.Join(directory, "evidence.sock")
		active, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		defer active.Close()
		if _, err := listenUnixAt(path); !errors.Is(err, ErrInvalidSocketPath) {
			t.Fatalf("error = %v", err)
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSocket == 0 {
			t.Fatalf("active socket changed: %#v, %v", info, err)
		}
	})

	t.Run("regular object", func(t *testing.T) {
		directory := newSocketDirectory(t)
		path := filepath.Join(directory, "evidence.sock")
		if err := os.WriteFile(path, []byte("must-remain"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := listenUnixAt(path); !errors.Is(err, ErrInvalidSocketPath) {
			t.Fatalf("error = %v", err)
		}
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "must-remain" {
			t.Fatalf("regular object changed: %q, %v", content, err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		directory := newSocketDirectory(t)
		target := filepath.Join(directory, "target")
		if err := os.WriteFile(target, []byte("must-remain"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "evidence.sock")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := listenUnixAt(path); !errors.Is(err, ErrInvalidSocketPath) {
			t.Fatalf("error = %v", err)
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlink changed: %#v, %v", info, err)
		}
	})

	t.Run("directory without setgid", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "socket")
		if err := os.Mkdir(directory, 0o770); err != nil {
			t.Fatal(err)
		}
		if _, err := listenUnixAt(filepath.Join(directory, "evidence.sock")); !errors.Is(err, ErrInvalidSocketPath) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("directory without group rwx", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "socket")
		if err := os.Mkdir(directory, 0o770); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o750|os.ModeSetgid); err != nil {
			t.Fatal(err)
		}
		if _, err := listenUnixAt(filepath.Join(directory, "evidence.sock")); !errors.Is(err, ErrInvalidSocketPath) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("symlink directory", func(t *testing.T) {
		directory := newSocketDirectory(t)
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(directory, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := listenUnixAt(filepath.Join(alias, "evidence.sock")); !errors.Is(err, ErrInvalidSocketPath) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestServeUnixListenerServesAuthenticatedHTTPAndCleansUp(t *testing.T) {
	directory := newSocketDirectory(t)
	path := filepath.Join(directory, "evidence.sock")
	listener, err := listenUnixAt(path)
	if err != nil {
		t.Fatal(err)
	}
	handler, token := newTestHandler(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- serveUnixListener(ctx, listener, handler) }()

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+string(token.encoded[:]))
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("response = %d/%q, %v", response.StatusCode, body, err)
	}
	if err := validateServiceStatus(body); err != nil {
		t.Fatal(err)
	}
	var status evidencev1alpha1.ServiceStatus
	if err := json.Unmarshal(body, &status); err != nil || status.Status != evidencev1alpha1.HealthLive {
		t.Fatalf("health = %#v, %v", status, err)
	}

	transport.CloseIdleConnections()
	cancel()
	select {
	case err := <-serveErrors:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Unix server did not shut down")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remained after shutdown: %v", err)
	}
}

type canaryContextKey struct{}

func TestUnixHTTPServerUsesFixedBoundsAndCallerContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), canaryContextKey{}, "canary")
	server := newUnixHTTPServer(ctx, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if server.ReadHeaderTimeout != RequestTimeout || server.ReadTimeout != RequestTimeout || server.WriteTimeout != RequestTimeout || server.IdleTimeout != serverIdleTimeout || server.MaxHeaderBytes != serverHeaderBytes || server.ErrorLog == nil {
		t.Fatalf("server bounds = %#v", server)
	}
	if got := server.BaseContext(nil); got != ctx {
		t.Fatal("server replaced the caller runtime context")
	}
}

// TestValidateSocketDirectoryAcceptsKubeletEffectiveMode pins the mode a
// real kubelet actually delivers: fsGroup application ORs 0770|setgid onto
// the emptyDir's initial 0777, yielding 02777 — observed live on OpenShift
// 4.20 under restricted-v2. The directory mode is a floor, not an exact
// match, because confinement comes from the mount set and no non-root
// container can strip the world bits from a root-owned directory.
func TestValidateSocketDirectoryAcceptsKubeletEffectiveMode(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "socket")
	if err := os.Mkdir(directory, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o777|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	if err := validateSocketDirectory(directory); err != nil {
		t.Fatalf("kubelet-effective 02777 must validate: %v", err)
	}
	path := filepath.Join(directory, "evidence.sock")
	listener, err := listenUnixAt(path)
	if err != nil {
		t.Fatalf("listen under kubelet-effective mode: %v", err)
	}
	defer listener.Close()
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != socketMode {
		t.Fatalf("socket mode = %v, %v; want 0660", info.Mode(), err)
	}
}

func newSocketDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "socket")
	if err := os.Mkdir(directory, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, socketDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := validateSocketDirectory(directory); err != nil {
		t.Fatal(err)
	}
	return directory
}
