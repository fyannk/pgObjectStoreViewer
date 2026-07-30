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
	"time"

	evidencev1alpha1 "github.com/fyannk/pgObjectStoreViewer/api/evidence/v1alpha1"
)

const (
	ProbeTimeout      = 2 * time.Second
	maximumProbeBytes = 4096
)

var ErrProbeFailed = errors.New("evidence liveness probe failed")

// ProbeHealth calls only authenticated liveness over the fixed Unix socket.
// Every failure is collapsed to ErrProbeFailed so neither transport nor body
// details can cross the exec-probe boundary.
func ProbeHealth(ctx context.Context, token Token) error {
	return probeHealthAt(ctx, token, SocketPath)
}

func probeHealthAt(ctx context.Context, token Token, socketPath string) error {
	if ctx == nil || !token.valid() || socketPath == "" {
		return ErrProbeFailed
	}
	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	transport := &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: ProbeTimeout}
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://unix/healthz", nil)
	if err != nil {
		return ErrProbeFailed
	}
	request.Header.Set("Authorization", "Bearer "+string(token.encoded[:]))
	response, err := client.Do(request)
	if err != nil {
		return ErrProbeFailed
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumProbeBytes+1))
	if err != nil || len(body) > maximumProbeBytes || response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != MediaType {
		return ErrProbeFailed
	}
	var status evidencev1alpha1.ServiceStatus
	if err := json.Unmarshal(body, &status); err != nil || status.Validate() != nil || status.Kind != evidencev1alpha1.HealthKind || status.Status != evidencev1alpha1.HealthLive {
		return ErrProbeFailed
	}
	return nil
}
