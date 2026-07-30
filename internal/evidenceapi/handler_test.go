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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	evidencev1alpha1 "github.com/fyannk/pgObjectStoreViewer/api/evidence/v1alpha1"
	"github.com/fyannk/pgObjectStoreViewer/internal/inventory"
	"github.com/fyannk/pgObjectStoreViewer/internal/readiness"
)

func TestEvidenceHandlerServesClosedAuthenticatedRouteSet(t *testing.T) {
	handler, token := newTestHandler(t, true)
	tests := []struct {
		name       string
		path       string
		status     int
		kind       string
		validation func([]byte) error
	}{
		{name: "health", path: "/healthz", status: http.StatusOK, kind: evidencev1alpha1.HealthKind, validation: validateServiceStatus},
		{name: "ready", path: "/readyz", status: http.StatusOK, kind: evidencev1alpha1.ReadinessKind, validation: validateServiceStatus},
		{name: "snapshot", path: "/api/v1alpha1/snapshot", status: http.StatusOK, kind: evidencev1alpha1.SnapshotKind, validation: validateSnapshot},
		{name: "backups", path: "/api/v1alpha1/backups?revision=7&limit=1", status: http.StatusOK, kind: evidencev1alpha1.BarmanBackupPageKind, validation: validateBackupPage},
		{name: "WAL ranges", path: "/api/v1alpha1/wal-ranges?revision=7", status: http.StatusOK, kind: evidencev1alpha1.BarmanWALRangePageKind, validation: validateWALRangePage},
		{name: "WAL gaps", path: "/api/v1alpha1/wal-gaps?revision=7", status: http.StatusOK, kind: evidencev1alpha1.BarmanWALGapPageKind, validation: validateWALGapPage},
		{name: "recovery paths", path: "/api/v1alpha1/recovery-paths?revision=7", status: http.StatusOK, kind: evidencev1alpha1.BarmanRecoveryPageKind, validation: validateRecoveryPage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(handler, token, http.MethodGet, test.path, nil)
			if response.Code != test.status {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			assertEvidenceHeaders(t, response.Header())
			if err := test.validation(response.Body.Bytes()); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(response.Body.String(), "artifact-key-canary") {
				t.Fatal("raw object key crossed the API boundary")
			}
			var envelope struct {
				APIVersion string `json:"api_version"`
				Kind       string `json:"kind"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.APIVersion != evidencev1alpha1.APIVersion || envelope.Kind != test.kind {
				t.Fatalf("envelope = %#v, %v", envelope, err)
			}
		})
	}

	notReady, notReadyToken := newTestHandler(t, false)
	response := performRequest(notReady, notReadyToken, http.MethodGet, "/readyz", nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready status = %d", response.Code)
	}
	var status evidencev1alpha1.ServiceStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || status.Status != evidencev1alpha1.ReadinessNotReady || status.Kind != evidencev1alpha1.ReadinessKind {
		t.Fatalf("not-ready response = %#v, %v", status, err)
	}
}

func TestEvidenceHandlerRejectsAuthenticationWithoutDisclosure(t *testing.T) {
	logs := &bytes.Buffer{}
	engine := newTestEngine(t, completeSource(t), 0x92)
	token := testToken(t, 0x41)
	handler, err := NewHandler(HandlerOptions{Engine: engine, Readiness: testReadiness(true), Token: token, Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	valid := "Bearer " + string(token.encoded[:])
	tests := []struct {
		name   string
		values []string
	}{
		{name: "missing"},
		{name: "wrong", values: []string{"Bearer " + strings.Repeat("A", encodedTokenBytes)}},
		{name: "wrong scheme", values: []string{"Basic " + string(token.encoded[:])}},
		{name: "oversized", values: []string{"Bearer " + strings.Repeat("A", maximumTokenFile+1)}},
		{name: "duplicate", values: []string{valid, valid}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1alpha1/snapshot", nil)
			for _, value := range test.values {
				request.Header.Add("Authorization", value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("status/header = %d/%q", response.Code, response.Header().Get("WWW-Authenticate"))
			}
			assertEvidenceHeaders(t, response.Header())
			assertAPIError(t, response.Body.Bytes(), evidencev1alpha1.ErrorUnauthenticated)
		})
	}
	canaryRequest := httptest.NewRequest(http.MethodGet, "/api/v1alpha1/backups?revision=7&cursor=cursor-canary-secret", nil)
	canaryRequest.Header.Set("Authorization", valid)
	canaryRequest.Header.Set("X-Forwarded-User", "identity-canary-secret")
	canaryResponse := httptest.NewRecorder()
	handler.ServeHTTP(canaryResponse, canaryRequest)
	canary := string(token.encoded[:])
	for _, secret := range []string{canary, "cursor-canary-secret", "identity-canary-secret"} {
		if strings.Contains(logs.String(), secret) || strings.Contains(canaryResponse.Body.String(), secret) {
			t.Fatalf("secret %q crossed the channel boundary", secret)
		}
	}
}

func TestEvidenceHandlerRejectsMethodsRoutesBodiesAndQueries(t *testing.T) {
	handler, token := newTestHandler(t, true)
	tests := []struct {
		name   string
		method string
		path   string
		body   io.Reader
		status int
		code   evidencev1alpha1.ErrorCode
	}{
		{name: "other method", method: http.MethodPost, path: "/api/v1alpha1/snapshot", status: http.StatusMethodNotAllowed, code: evidencev1alpha1.ErrorMethodNotAllowed},
		{name: "unknown route", method: http.MethodGet, path: "/api/v1alpha1/objects/secret-key", status: http.StatusNotFound, code: evidencev1alpha1.ErrorNotFound},
		{name: "health query", method: http.MethodGet, path: "/healthz?detail=true", status: http.StatusBadRequest, code: evidencev1alpha1.ErrorInvalidRequest},
		{name: "snapshot query", method: http.MethodGet, path: "/api/v1alpha1/snapshot?key=secret", status: http.StatusBadRequest, code: evidencev1alpha1.ErrorInvalidRequest},
		{name: "request body", method: http.MethodGet, path: "/api/v1alpha1/snapshot", body: strings.NewReader("ignored"), status: http.StatusBadRequest, code: evidencev1alpha1.ErrorInvalidRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(handler, token, test.method, test.path, test.body)
			if response.Code != test.status {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			assertEvidenceHeaders(t, response.Header())
			assertAPIError(t, response.Body.Bytes(), test.code)
			if response.Header().Get("Access-Control-Allow-Origin") != "" || response.Header().Get("Content-Encoding") != "" {
				t.Fatal("CORS or compression was enabled")
			}
		})
	}
}

func TestEvidenceHandlerStrictlyParsesBoundedPageQueries(t *testing.T) {
	handler, token := newTestHandler(t, true)
	invalid := []string{
		"", "revision=0", "revision=+7", "revision=-7", "revision=18446744073709551616",
		"revision=7&revision=7", "revision=7&unknown=x", "revision=7&limit=-1",
		"revision=7&limit=201", "revision=7&limit=1&limit=1", "revision=7&cursor=",
		"revision=7&cursor=a&cursor=a", "revision=7;limit=1",
	}
	for _, query := range invalid {
		response := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/backups?"+query, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d", query, response.Code)
		}
		assertAPIError(t, response.Body.Bytes(), evidencev1alpha1.ErrorInvalidRequest)
	}
	response := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/backups?revision=6", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("changed publication status = %d", response.Code)
	}
	assertAPIError(t, response.Body.Bytes(), evidencev1alpha1.ErrorPublicationChanged)
}

func TestEvidenceHandlerEnforcesResponseAndConcurrencyBounds(t *testing.T) {
	token := testToken(t, 0x43)
	baseLimits := productionHandlerLimits()
	baseLimits.maximumSnapshot = 1
	handler, err := newHandler(engineSource{engine: newTestEngine(t, completeSource(t), 0x93)}, func() bool { return true }, token, discardLogger(), baseLimits)
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/snapshot", nil)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response limit status = %d", response.Code)
	}
	assertAPIError(t, response.Body.Bytes(), evidencev1alpha1.ErrorResponseLimit)

	collectionLimits := productionHandlerLimits()
	collectionLimits.maximumCollection = 1
	handler, err = newHandler(engineSource{engine: newTestEngine(t, completeSource(t), 0x94)}, func() bool { return true }, token, discardLogger(), collectionLimits)
	if err != nil {
		t.Fatal(err)
	}
	response = performRequest(handler, token, http.MethodGet, "/api/v1alpha1/backups?revision=7", nil)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("collection response limit status = %d", response.Code)
	}
	assertAPIError(t, response.Body.Bytes(), evidencev1alpha1.ErrorResponseLimit)

	publication := mustProject(t, completeSource(t))
	blocking := &blockingSource{snapshot: publication.Snapshot, entered: make(chan struct{}, 2), release: make(chan struct{})}
	limits := productionHandlerLimits()
	limits.maximumConcurrent = 2
	handler, err = newHandler(blocking, func() bool { return true }, token, discardLogger(), limits)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = performRequest(handler, token, http.MethodGet, "/api/v1alpha1/snapshot", nil)
		}()
	}
	for range 2 {
		<-blocking.entered
	}
	busy := performRequest(handler, token, http.MethodGet, "/healthz", nil)
	if busy.Code != http.StatusTooManyRequests {
		t.Fatalf("busy status = %d", busy.Code)
	}
	assertAPIError(t, busy.Body.Bytes(), evidencev1alpha1.ErrorBusy)
	close(blocking.release)
	wait.Wait()
}

func TestEvidenceHandlerRejectsInvalidCollectionBeforeEncoding(t *testing.T) {
	token := testToken(t, 0x45)
	engine := newTestEngine(t, completeSource(t), 0x95)
	handler, err := newHandler(invalidCollectionSource{engineSource: engineSource{engine: engine}}, func() bool { return true }, token, discardLogger(), productionHandlerLimits())
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/backups?revision=7", nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("invalid collection status = %d", response.Code)
	}
	assertAPIError(t, response.Body.Bytes(), evidencev1alpha1.ErrorInvalidPublication)
}

func TestEvidenceHandlerAppliesDeadlineAndCancellation(t *testing.T) {
	token := testToken(t, 0x44)
	source := &deadlineSource{}
	limits := productionHandlerLimits()
	limits.requestTimeout = 20 * time.Millisecond
	handler, err := newHandler(source, func() bool { return true }, token, discardLogger(), limits)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	response := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/snapshot", nil)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("deadline took %s", elapsed)
	}
	if response.Code != http.StatusInternalServerError || !source.sawDeadline.Load() {
		t.Fatalf("deadline response = %d, observed = %t", response.Code, source.sawDeadline.Load())
	}
	assertAPIError(t, response.Body.Bytes(), evidencev1alpha1.ErrorInvalidPublication)
}

func TestEvidenceHandlerReturnsMutationIsolatedDeterministicJSON(t *testing.T) {
	handler, token := newTestHandler(t, true)
	first := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/backups?revision=7&limit=1", nil)
	second := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/backups?revision=7&limit=1", nil)
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("same publication request produced different JSON")
	}
	var page evidencev1alpha1.BarmanBackupPage
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	*page.Items[0].Status = "MUTATED"
	third := performRequest(handler, token, http.MethodGet, "/api/v1alpha1/backups?revision=7&limit=1", nil)
	if !bytes.Equal(first.Body.Bytes(), third.Body.Bytes()) {
		t.Fatal("consumer mutation changed publication JSON")
	}
}

func newTestHandler(t *testing.T, ready bool) (*Handler, Token) {
	t.Helper()
	token := testToken(t, 0x40)
	handler, err := NewHandler(HandlerOptions{
		Engine:    newTestEngine(t, sourceWithBackups(t, 3), 0x91),
		Readiness: testReadiness(ready), Token: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, token
}

func testToken(t *testing.T, value byte) Token {
	t.Helper()
	encoded := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, decodedTokenBytes))
	token, err := parseToken([]byte(encoded))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func testReadiness(ready bool) *readiness.ProbeState {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	state := readiness.New(true, time.Hour, func() time.Time { return now })
	if ready {
		state.MarkReachable(now)
	}
	return state
}

func performRequest(handler http.Handler, token Token, method, target string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+string(token.encoded[:]))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertEvidenceHeaders(t *testing.T, header http.Header) {
	t.Helper()
	want := map[string]string{
		"Content-Type": MediaType, "Cache-Control": "no-store",
		"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer",
	}
	for key, value := range want {
		if header.Get(key) != value {
			t.Fatalf("%s = %q, want %q", key, header.Get(key), value)
		}
	}
	if header.Get("Content-Encoding") != "" {
		t.Fatal("response was compressed")
	}
}

func assertAPIError(t *testing.T, body []byte, code evidencev1alpha1.ErrorCode) {
	t.Helper()
	var value evidencev1alpha1.EvidenceAPIError
	if err := json.Unmarshal(body, &value); err != nil || value.Validate() != nil || value.Code != code {
		t.Fatalf("API error = %#v, %v", value, err)
	}
}

func validateServiceStatus(body []byte) error {
	var value evidencev1alpha1.ServiceStatus
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return value.Validate()
}

func validateSnapshot(body []byte) error {
	var value evidencev1alpha1.RepositoryEvidenceSnapshot
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return value.Validate()
}

func validateBackupPage(body []byte) error {
	var value evidencev1alpha1.BarmanBackupPage
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return value.Validate()
}

func validateWALRangePage(body []byte) error {
	var value evidencev1alpha1.BarmanWALRangePage
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return value.Validate()
}

func validateWALGapPage(body []byte) error {
	var value evidencev1alpha1.BarmanWALGapPage
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return value.Validate()
}

func validateRecoveryPage(body []byte) error {
	var value evidencev1alpha1.BarmanRecoveryPathPage
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return value.Validate()
}

func mustProject(t *testing.T, source inventory.Snapshot) Publication {
	t.Helper()
	publication, err := Project(source, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	return publication
}

type blockingSource struct {
	snapshot evidencev1alpha1.RepositoryEvidenceSnapshot
	entered  chan struct{}
	release  chan struct{}
}

func (s *blockingSource) Snapshot(ctx context.Context) (evidencev1alpha1.RepositoryEvidenceSnapshot, error) {
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return s.snapshot, nil
	case <-ctx.Done():
		return evidencev1alpha1.RepositoryEvidenceSnapshot{}, ctx.Err()
	}
}

func (s *blockingSource) Backups(context.Context, PageRequest) (evidencev1alpha1.BarmanBackupPage, error) {
	return evidencev1alpha1.BarmanBackupPage{}, errors.New("unexpected call")
}

func (s *blockingSource) WALRanges(context.Context, PageRequest) (evidencev1alpha1.BarmanWALRangePage, error) {
	return evidencev1alpha1.BarmanWALRangePage{}, errors.New("unexpected call")
}

func (s *blockingSource) WALGaps(context.Context, PageRequest) (evidencev1alpha1.BarmanWALGapPage, error) {
	return evidencev1alpha1.BarmanWALGapPage{}, errors.New("unexpected call")
}

func (s *blockingSource) RecoveryPaths(context.Context, PageRequest) (evidencev1alpha1.BarmanRecoveryPathPage, error) {
	return evidencev1alpha1.BarmanRecoveryPathPage{}, errors.New("unexpected call")
}

type deadlineSource struct {
	sawDeadline atomic.Bool
}

type invalidCollectionSource struct{ engineSource }

func (invalidCollectionSource) Backups(context.Context, PageRequest) (evidencev1alpha1.BarmanBackupPage, error) {
	return evidencev1alpha1.BarmanBackupPage{}, nil
}

func (s *deadlineSource) Snapshot(ctx context.Context) (evidencev1alpha1.RepositoryEvidenceSnapshot, error) {
	if _, ok := ctx.Deadline(); ok {
		s.sawDeadline.Store(true)
	}
	<-ctx.Done()
	return evidencev1alpha1.RepositoryEvidenceSnapshot{}, ctx.Err()
}

func (s *deadlineSource) Backups(context.Context, PageRequest) (evidencev1alpha1.BarmanBackupPage, error) {
	return evidencev1alpha1.BarmanBackupPage{}, errors.New("unexpected call")
}

func (s *deadlineSource) WALRanges(context.Context, PageRequest) (evidencev1alpha1.BarmanWALRangePage, error) {
	return evidencev1alpha1.BarmanWALRangePage{}, errors.New("unexpected call")
}

func (s *deadlineSource) WALGaps(context.Context, PageRequest) (evidencev1alpha1.BarmanWALGapPage, error) {
	return evidencev1alpha1.BarmanWALGapPage{}, errors.New("unexpected call")
}

func (s *deadlineSource) RecoveryPaths(context.Context, PageRequest) (evidencev1alpha1.BarmanRecoveryPathPage, error) {
	return evidencev1alpha1.BarmanRecoveryPathPage{}, errors.New("unexpected call")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
