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
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"
)

const (
	SocketPath = "/var/run/objectstoreviewer/evidence.sock"
	// socketDirectoryMode is the minimum the directory must grant: setgid
	// so the socket inherits the directory's group, and group rwx so both
	// containers can create and connect under arbitrary UIDs. It is a
	// floor, not an exact match: the kubelet applies fsGroup by OR-ing
	// exactly these bits onto the emptyDir's initial 0777, so the
	// effective mode observed on conformant kubelets is 02777 — proven
	// live on OpenShift 4.20 under restricted-v2. The surplus world bits
	// are inert: per the evidence contract, confinement comes from the
	// mount set (only pgConsole and the viewer carry the volume), never
	// from the file mode, and no non-root container could strip them from
	// a root-owned directory anyway.
	socketDirectoryMode = os.FileMode(0o770) | os.ModeSetgid
	socketMode          = os.FileMode(0o660)
	serverHeaderBytes   = 16 * 1024
	serverIdleTimeout   = 30 * time.Second
	serverShutdown      = 15 * time.Second
)

var ErrInvalidSocketPath = errors.New("evidence socket path is invalid")

func listenUnixAt(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." {
		return nil, ErrInvalidSocketPath
	}
	if err := validateSocketDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
			return nil, ErrInvalidSocketPath
		}
		connection, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return nil, ErrInvalidSocketPath
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, ErrInvalidSocketPath
		}
		if err := os.Remove(path); err != nil {
			return nil, ErrInvalidSocketPath
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrInvalidSocketPath
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, ErrInvalidSocketPath
	}
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(path, socketMode); err != nil {
		_ = listener.Close()
		return nil, ErrInvalidSocketPath
	}
	info, err := os.Lstat(path)
	directoryInfo, directoryErr := os.Lstat(filepath.Dir(path))
	if err != nil || directoryErr != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != socketMode || !sameGroup(info, directoryInfo) {
		_ = listener.Close()
		return nil, ErrInvalidSocketPath
	}
	return listener, nil
}

func sameGroup(left, right os.FileInfo) bool {
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	return leftOK && rightOK && leftStat.Gid == rightStat.Gid
}

func validateSocketDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&socketDirectoryMode.Perm() != socketDirectoryMode.Perm() ||
		info.Mode()&os.ModeSetgid == 0 {
		return ErrInvalidSocketPath
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ErrInvalidSocketPath
	}
	groups, err := os.Getgroups()
	if err != nil {
		return ErrInvalidSocketPath
	}
	if os.Getegid() != int(stat.Gid) && !slices.Contains(groups, int(stat.Gid)) {
		return ErrInvalidSocketPath
	}
	return nil
}

// ServeUnix serves the closed evidence handler over the fixed private socket
// until cancellation.
func ServeUnix(ctx context.Context, handler *Handler) error {
	listener, err := listenUnixAt(SocketPath)
	if err != nil {
		return err
	}
	return serveUnixListener(ctx, listener, handler)
}

func serveUnixListener(ctx context.Context, listener net.Listener, handler http.Handler) error {
	if ctx == nil || listener == nil || handler == nil {
		return errors.New("evidence Unix server configuration is invalid")
	}
	defer func() { _ = listener.Close() }()
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	defer cancelRuntime()
	server := newUnixHTTPServer(runtimeCtx, handler)
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdown)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		err := <-serveErrors
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newUnixHTTPServer(runtimeCtx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler: handler, ReadHeaderTimeout: RequestTimeout, ReadTimeout: RequestTimeout,
		WriteTimeout: RequestTimeout, IdleTimeout: serverIdleTimeout, MaxHeaderBytes: serverHeaderBytes,
		BaseContext: func(net.Listener) context.Context { return runtimeCtx },
		ErrorLog:    log.New(io.Discard, "", 0),
	}
}
