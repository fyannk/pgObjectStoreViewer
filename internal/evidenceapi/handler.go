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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	evidencev1alpha1 "github.com/fyannk/objectstoreviewer/api/evidence/v1alpha1"
	"github.com/fyannk/objectstoreviewer/internal/readiness"
)

const (
	MediaType                 = "application/vnd.objectstoreviewer.evidence.v1alpha1+json"
	MaximumSnapshotBytes      = 256 * 1024
	MaximumCollectionBytes    = 1024 * 1024
	MaximumConcurrentRequests = 16
	RequestTimeout            = 5 * time.Second
	maximumQueryBytes         = MaximumCursorBytes + 256
)

// HandlerOptions contains only the in-memory evidence engine and readiness
// state plus the pod-local channel token. It cannot reach a store or start a
// scan.
type HandlerOptions struct {
	Engine    *Engine
	Readiness *readiness.ProbeState
	Token     Token
	Logger    *slog.Logger
}

// Handler serves the closed v1alpha1 evidence route set.
type Handler struct {
	source      publicationSource
	readiness   func() bool
	token       Token
	logger      *slog.Logger
	concurrency chan struct{}
	requestID   atomic.Uint64
	limits      handlerLimits
}

type publicationSource interface {
	Snapshot(context.Context) (evidencev1alpha1.RepositoryEvidenceSnapshot, error)
	Backups(context.Context, PageRequest) (evidencev1alpha1.BarmanBackupPage, error)
	WALRanges(context.Context, PageRequest) (evidencev1alpha1.BarmanWALRangePage, error)
	WALGaps(context.Context, PageRequest) (evidencev1alpha1.BarmanWALGapPage, error)
	RecoveryPaths(context.Context, PageRequest) (evidencev1alpha1.BarmanRecoveryPathPage, error)
}

type engineSource struct {
	engine *Engine
}

func (s engineSource) Snapshot(ctx context.Context) (evidencev1alpha1.RepositoryEvidenceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return evidencev1alpha1.RepositoryEvidenceSnapshot{}, err
	}
	return s.engine.Snapshot(), nil
}

func (s engineSource) Backups(ctx context.Context, request PageRequest) (evidencev1alpha1.BarmanBackupPage, error) {
	if err := ctx.Err(); err != nil {
		return evidencev1alpha1.BarmanBackupPage{}, err
	}
	return s.engine.Backups(request)
}

func (s engineSource) WALRanges(ctx context.Context, request PageRequest) (evidencev1alpha1.BarmanWALRangePage, error) {
	if err := ctx.Err(); err != nil {
		return evidencev1alpha1.BarmanWALRangePage{}, err
	}
	return s.engine.WALRanges(request)
}

func (s engineSource) WALGaps(ctx context.Context, request PageRequest) (evidencev1alpha1.BarmanWALGapPage, error) {
	if err := ctx.Err(); err != nil {
		return evidencev1alpha1.BarmanWALGapPage{}, err
	}
	return s.engine.WALGaps(request)
}

func (s engineSource) RecoveryPaths(ctx context.Context, request PageRequest) (evidencev1alpha1.BarmanRecoveryPathPage, error) {
	if err := ctx.Err(); err != nil {
		return evidencev1alpha1.BarmanRecoveryPathPage{}, err
	}
	return s.engine.RecoveryPaths(request)
}

type handlerLimits struct {
	requestTimeout    time.Duration
	maximumSnapshot   int
	maximumCollection int
	maximumConcurrent int
}

func productionHandlerLimits() handlerLimits {
	return handlerLimits{
		requestTimeout: RequestTimeout, maximumSnapshot: MaximumSnapshotBytes,
		maximumCollection: MaximumCollectionBytes, maximumConcurrent: MaximumConcurrentRequests,
	}
}

// NewHandler constructs the authenticated, provider-free v1alpha1 handler.
func NewHandler(options HandlerOptions) (*Handler, error) {
	if options.Engine == nil || options.Readiness == nil || !options.Token.valid() {
		return nil, errors.New("evidence API handler configuration is invalid")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return newHandler(engineSource{engine: options.Engine}, func() bool { return options.Readiness.Result().Ready }, options.Token, logger, productionHandlerLimits())
}

func newHandler(source publicationSource, readiness func() bool, token Token, logger *slog.Logger, limits handlerLimits) (*Handler, error) {
	if source == nil || readiness == nil || !token.valid() || logger == nil || limits.requestTimeout <= 0 || limits.maximumSnapshot <= 0 || limits.maximumCollection <= 0 || limits.maximumConcurrent <= 0 {
		return nil, errors.New("evidence API handler configuration is invalid")
	}
	return &Handler{
		source: source, readiness: readiness, token: token, logger: logger,
		concurrency: make(chan struct{}, limits.maximumConcurrent), limits: limits,
	}, nil
}

// ServeHTTP authenticates before dispatch and never delegates outside the
// closed evidence route set.
func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	requestID := fmt.Sprintf("evidence-%016x", h.requestID.Add(1))
	route := routeName(request.URL.Path)
	revision := uint64(0)

	if !h.authenticated(request) {
		result := apiError(http.StatusUnauthorized, evidencev1alpha1.ErrorUnauthenticated)
		result.headers = map[string]string{"WWW-Authenticate": "Bearer"}
		h.write(writer, result)
		h.log(requestID, route, result, revision, started)
		return
	}

	select {
	case h.concurrency <- struct{}{}:
		defer func() { <-h.concurrency }()
	default:
		result := apiError(http.StatusTooManyRequests, evidencev1alpha1.ErrorBusy)
		h.write(writer, result)
		h.log(requestID, route, result, revision, started)
		return
	}

	if request.Method != http.MethodGet {
		result := apiError(http.StatusMethodNotAllowed, evidencev1alpha1.ErrorMethodNotAllowed)
		result.headers = map[string]string{"Allow": http.MethodGet}
		h.write(writer, result)
		h.log(requestID, route, result, revision, started)
		return
	}
	if route == "not-found" {
		result := apiError(http.StatusNotFound, evidencev1alpha1.ErrorNotFound)
		h.write(writer, result)
		h.log(requestID, route, result, revision, started)
		return
	}
	if request.ContentLength != 0 || len(request.TransferEncoding) != 0 || len(request.URL.RawQuery) > maximumQueryBytes {
		result := apiError(http.StatusBadRequest, evidencev1alpha1.ErrorInvalidRequest)
		h.write(writer, result)
		h.log(requestID, route, result, revision, started)
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), h.limits.requestTimeout)
	defer cancel()
	request = request.WithContext(ctx)
	result, parsedRevision := h.dispatch(request)
	revision = parsedRevision
	if request.Context().Err() != nil && errors.Is(request.Context().Err(), context.Canceled) {
		return
	}
	h.write(writer, result)
	h.log(requestID, route, result, revision, started)
}

func (h *Handler) dispatch(request *http.Request) (handlerResponse, uint64) {
	switch request.URL.Path {
	case "/healthz":
		if request.URL.RawQuery != "" {
			return apiError(http.StatusBadRequest, evidencev1alpha1.ErrorInvalidRequest), 0
		}
		return jsonResponse(http.StatusOK, evidencev1alpha1.ServiceStatus{APIVersion: evidencev1alpha1.APIVersion, Kind: evidencev1alpha1.HealthKind, Status: evidencev1alpha1.HealthLive}, h.limits.maximumSnapshot), 0
	case "/readyz":
		if request.URL.RawQuery != "" {
			return apiError(http.StatusBadRequest, evidencev1alpha1.ErrorInvalidRequest), 0
		}
		status := http.StatusOK
		body := evidencev1alpha1.ServiceStatus{APIVersion: evidencev1alpha1.APIVersion, Kind: evidencev1alpha1.ReadinessKind, Status: evidencev1alpha1.ReadinessReady}
		if !h.readiness() {
			status = http.StatusServiceUnavailable
			body.Status = evidencev1alpha1.ReadinessNotReady
		}
		return jsonResponse(status, body, h.limits.maximumSnapshot), 0
	case "/api/v1alpha1/snapshot":
		if request.URL.RawQuery != "" {
			return apiError(http.StatusBadRequest, evidencev1alpha1.ErrorInvalidRequest), 0
		}
		value, err := h.source.Snapshot(request.Context())
		if err != nil || value.Validate() != nil {
			return apiError(http.StatusInternalServerError, evidencev1alpha1.ErrorInvalidPublication), 0
		}
		return jsonResponse(http.StatusOK, value, h.limits.maximumSnapshot), value.Revision
	case "/api/v1alpha1/backups", "/api/v1alpha1/wal-ranges", "/api/v1alpha1/wal-gaps", "/api/v1alpha1/recovery-paths":
		pageRequest, err := parsePageRequest(request.URL.RawQuery)
		if err != nil {
			return apiError(http.StatusBadRequest, evidencev1alpha1.ErrorInvalidRequest), 0
		}
		return h.collection(request.Context(), request.URL.Path, pageRequest), pageRequest.Revision
	default:
		return apiError(http.StatusNotFound, evidencev1alpha1.ErrorNotFound), 0
	}
}

func (h *Handler) collection(ctx context.Context, path string, request PageRequest) handlerResponse {
	var value any
	var err error
	switch path {
	case "/api/v1alpha1/backups":
		value, err = h.source.Backups(ctx, request)
	case "/api/v1alpha1/wal-ranges":
		value, err = h.source.WALRanges(ctx, request)
	case "/api/v1alpha1/wal-gaps":
		value, err = h.source.WALGaps(ctx, request)
	case "/api/v1alpha1/recovery-paths":
		value, err = h.source.RecoveryPaths(ctx, request)
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRequest):
			return apiError(http.StatusBadRequest, evidencev1alpha1.ErrorInvalidRequest)
		case errors.Is(err, ErrPublicationChanged):
			return apiError(http.StatusConflict, evidencev1alpha1.ErrorPublicationChanged)
		default:
			return apiError(http.StatusInternalServerError, evidencev1alpha1.ErrorInvalidPublication)
		}
	}
	if err := validateCollection(value); err != nil {
		return apiError(http.StatusInternalServerError, evidencev1alpha1.ErrorInvalidPublication)
	}
	return jsonResponse(http.StatusOK, value, h.limits.maximumCollection)
}

func validateCollection(value any) error {
	switch page := value.(type) {
	case evidencev1alpha1.BarmanBackupPage:
		return page.Validate()
	case evidencev1alpha1.BarmanWALRangePage:
		return page.Validate()
	case evidencev1alpha1.BarmanWALGapPage:
		return page.Validate()
	case evidencev1alpha1.BarmanRecoveryPathPage:
		return page.Validate()
	default:
		return ErrInvalidPublication
	}
}

func parsePageRequest(rawQuery string) (PageRequest, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return PageRequest{}, ErrInvalidRequest
	}
	for key, values := range query {
		if key != "revision" && key != "limit" && key != "cursor" || len(values) != 1 {
			return PageRequest{}, ErrInvalidRequest
		}
	}
	revision, ok := singleDecimal(query, "revision", true)
	if !ok || revision == 0 {
		return PageRequest{}, ErrInvalidRequest
	}
	limit, ok := singleDecimal(query, "limit", false)
	if !ok || limit > MaximumPageSize {
		return PageRequest{}, ErrInvalidRequest
	}
	cursor := ""
	if values, exists := query["cursor"]; exists {
		if len(values) != 1 || values[0] == "" || len(values[0]) > MaximumCursorBytes {
			return PageRequest{}, ErrInvalidRequest
		}
		cursor = values[0]
	}
	return PageRequest{Revision: revision, Limit: int(limit), Cursor: cursor}, nil
}

func singleDecimal(query url.Values, key string, required bool) (uint64, bool) {
	values, exists := query[key]
	if !exists {
		return 0, !required
	}
	if len(values) != 1 || values[0] == "" || strings.IndexFunc(values[0], func(value rune) bool { return value < '0' || value > '9' }) != -1 {
		return 0, false
	}
	value, err := strconv.ParseUint(values[0], 10, 64)
	return value, err == nil
}

func (h *Handler) authenticated(request *http.Request) bool {
	values := request.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) != len("Bearer ")+encodedTokenBytes || !strings.HasPrefix(values[0], "Bearer ") {
		return false
	}
	provided := values[0][len("Bearer "):]
	return subtle.ConstantTimeCompare([]byte(provided), h.token.encoded[:]) == 1
}

type handlerResponse struct {
	status  int
	body    []byte
	code    evidencev1alpha1.ErrorCode
	headers map[string]string
}

func jsonResponse(status int, value any, maximum int) handlerResponse {
	body, err := json.Marshal(value)
	if err != nil {
		return apiError(http.StatusInternalServerError, evidencev1alpha1.ErrorInvalidPublication)
	}
	if len(body) > maximum {
		return apiError(http.StatusRequestEntityTooLarge, evidencev1alpha1.ErrorResponseLimit)
	}
	return handlerResponse{status: status, body: body}
}

func apiError(status int, code evidencev1alpha1.ErrorCode) handlerResponse {
	messages := map[evidencev1alpha1.ErrorCode]string{
		evidencev1alpha1.ErrorInvalidRequest:     "invalid evidence request",
		evidencev1alpha1.ErrorUnauthenticated:    "authentication required",
		evidencev1alpha1.ErrorNotFound:           "evidence route not found",
		evidencev1alpha1.ErrorMethodNotAllowed:   "method not allowed",
		evidencev1alpha1.ErrorPublicationChanged: "repository evidence changed; restart from snapshot",
		evidencev1alpha1.ErrorResponseLimit:      "evidence response exceeds safety limit",
		evidencev1alpha1.ErrorBusy:               "evidence API is busy; retry later",
		evidencev1alpha1.ErrorInvalidPublication: "repository evidence unavailable",
	}
	value := evidencev1alpha1.EvidenceAPIError{APIVersion: evidencev1alpha1.APIVersion, Kind: evidencev1alpha1.ErrorKind, Code: code, Message: messages[code]}
	body, _ := json.Marshal(value)
	return handlerResponse{status: status, body: body, code: code}
}

func (h *Handler) write(writer http.ResponseWriter, response handlerResponse) {
	writer.Header().Set("Content-Type", MediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Content-Length", strconv.Itoa(len(response.body)))
	for key, value := range response.headers {
		writer.Header().Set(key, value)
	}
	writer.WriteHeader(response.status)
	_, _ = writer.Write(response.body)
}

func (h *Handler) log(requestID, route string, response handlerResponse, revision uint64, started time.Time) {
	attributes := []any{
		slog.String("request_id", requestID), slog.String("route", route),
		slog.Int("status", response.status), slog.Duration("duration", time.Since(started)),
	}
	if revision != 0 {
		attributes = append(attributes, slog.Uint64("revision", revision))
	}
	if response.code != "" {
		attributes = append(attributes, slog.String("error_code", string(response.code)))
	}
	h.logger.Info("evidence API request", attributes...)
}

func routeName(path string) string {
	switch path {
	case "/healthz":
		return "healthz"
	case "/readyz":
		return "readyz"
	case "/api/v1alpha1/snapshot":
		return "snapshot"
	case "/api/v1alpha1/backups":
		return "backups"
	case "/api/v1alpha1/wal-ranges":
		return "wal-ranges"
	case "/api/v1alpha1/wal-gaps":
		return "wal-gaps"
	case "/api/v1alpha1/recovery-paths":
		return "recovery-paths"
	default:
		return "not-found"
	}
}
