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

package inventory

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/fyannk/objectstoreviewer/internal/evidence"
	"github.com/fyannk/objectstoreviewer/internal/fault"
	"github.com/fyannk/objectstoreviewer/internal/formats/barmancloud"
	"github.com/fyannk/objectstoreviewer/internal/readiness"
	"github.com/fyannk/objectstoreviewer/internal/repository"
	"github.com/fyannk/objectstoreviewer/internal/store"
)

type Clock func() time.Time

// SnapshotCache is the scanner-owned publication surface. Implementations
// validate and replace snapshots atomically and preserve the previous snapshot
// when Publish fails.
type SnapshotCache interface {
	Load() Snapshot
	Publish(Snapshot) error
}

type ScannerOptions struct {
	Store                 store.Reader
	Format                repository.Format
	ConfiguredScopes      []string
	Cache                 SnapshotCache
	Readiness             *readiness.ProbeState
	RefreshInterval       time.Duration
	MaxObjects            int
	PageSize              int
	RecentLimit           int
	Now                   Clock
	Logger                *slog.Logger
	AnalyzeBarmanCatalog  bool
	BarmanRecoveryOptions barmancloud.RecoveryOptions
}

type Scanner struct {
	store                 store.Reader
	format                repository.Format
	configuredScopes      []string
	cache                 SnapshotCache
	readiness             *readiness.ProbeState
	refreshInterval       time.Duration
	maxObjects            int
	pageSize              int
	recentLimit           int
	now                   Clock
	logger                *slog.Logger
	analyzeBarmanCatalog  bool
	barmanRecoveryOptions barmancloud.RecoveryOptions

	refreshMu  sync.Mutex
	generation atomic.Uint64
}

func NewScanner(options ScannerOptions) (*Scanner, error) {
	if options.Store == nil || options.Format == nil || options.Cache == nil || options.Readiness == nil || options.Logger == nil ||
		options.RefreshInterval <= 0 || options.MaxObjects < 1 || options.PageSize < 1 || options.PageSize > store.MaxPageObjects ||
		options.RecentLimit < 1 || options.RecentLimit > MaxRecentObjects || len(options.ConfiguredScopes) > MaxScopes {
		return nil, errors.New("invalid inventory scanner options")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Scanner{
		store: options.Store, format: options.Format, configuredScopes: slices.Clone(options.ConfiguredScopes),
		cache: options.Cache, readiness: options.Readiness, refreshInterval: options.RefreshInterval,
		maxObjects: options.MaxObjects, pageSize: options.PageSize, recentLimit: options.RecentLimit, now: now,
		logger:                options.Logger,
		analyzeBarmanCatalog:  options.AnalyzeBarmanCatalog,
		barmanRecoveryOptions: options.BarmanRecoveryOptions,
	}, nil
}

func (s *Scanner) Run(ctx context.Context) {
	s.runCycle(ctx)
	ticker := time.NewTicker(s.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCycle(ctx)
		}
	}
}

func (s *Scanner) runCycle(ctx context.Context) {
	s.probe(ctx)
	if err := s.Refresh(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.WarnContext(ctx, "inventory refresh failed",
			slog.String("category", string(fault.Categorize(err))),
			slog.Uint64("refresh_generation", s.generation.Load()),
		)
	}
}

func (s *Scanner) Probe(ctx context.Context) fault.Category {
	return s.probe(ctx)
}

func (s *Scanner) probe(ctx context.Context) fault.Category {
	_, err := s.store.List(ctx, store.ListRequest{Limit: 1})
	now := s.now().UTC()
	if err != nil {
		category := fault.Categorize(err)
		s.readiness.MarkFailure(now, category)
		return category
	}
	s.readiness.MarkReachable(now)
	return fault.Unknown
}

func (s *Scanner) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	generation := s.generation.Add(1)
	started := s.now().UTC()
	snapshot, err := s.scan(ctx, generation, started)
	if err == nil {
		return s.cache.Publish(snapshot)
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	s.publishFailure(generation, started, fault.Categorize(err))
	return err
}

func (s *Scanner) scan(ctx context.Context, generation uint64, started time.Time) (Snapshot, error) {
	matcher, err := s.format.NewScopeMatcher(s.configuredScopes)
	if err != nil {
		return Snapshot{}, err
	}
	descriptor := s.format.Descriptor()
	builder, err := newBuilder(descriptor, matcher.InitialScopes(), s.recentLimit, s.analyzeBarmanCatalog)
	if err != nil {
		return Snapshot{}, err
	}
	cursor := ""
	maxPages := (s.maxObjects+s.pageSize-1)/s.pageSize + 100
	for pageNumber := 0; ; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if pageNumber >= maxPages {
			return Snapshot{}, &scanError{kind: fault.SafetyLimit}
		}
		remaining := s.maxObjects - int(builder.objectCount)
		if remaining <= 0 {
			return Snapshot{}, &scanError{kind: fault.SafetyLimit}
		}
		limit := min(s.pageSize, remaining)
		page, err := s.store.List(ctx, store.ListRequest{Cursor: cursor, Limit: limit})
		if err != nil {
			return Snapshot{}, err
		}
		if len(page.Objects) > limit || len(page.NextCursor) > store.MaxCursorBytes {
			return Snapshot{}, &scanError{kind: fault.SafetyLimit}
		}
		builder.pagesExamined++
		for _, object := range page.Objects {
			match, matched := matcher.Match(object.Key)
			if err := builder.add(object, match, matched); err != nil {
				return Snapshot{}, err
			}
		}
		if page.NextCursor == "" {
			break
		}
		if builder.objectCount >= int64(s.maxObjects) || page.NextCursor == cursor {
			return Snapshot{}, &scanError{kind: fault.SafetyLimit}
		}
		cursor = page.NextCursor
	}
	snapshot := builder.complete(generation, started, s.now().UTC())
	if s.analyzeBarmanCatalog && descriptor.ID == "barman-cloud" {
		snapshot.BarmanCatalog = barmancloud.Analyze(ctx, s.store, builder.catalogObjects)
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		applyBarmanEvidence(&snapshot)
		previous := s.cache.Load()
		snapshot.BarmanWAL = builder.barmanWAL.Finish(snapshot.BarmanCatalog.Backups, previous.BarmanWAL, generation)
		applyBarmanWALEvidence(&snapshot)
		historyObjects, historyTruncated := builder.barmanWAL.HistoryObjects()
		snapshot.BarmanRecovery = barmancloud.AnalyzeRecovery(ctx, s.store, snapshot.BarmanCatalog.Backups, snapshot.BarmanWAL, historyObjects, historyTruncated, s.barmanRecoveryOptions)
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		applyBarmanRecoveryEvidence(&snapshot)
	}
	return snapshot, nil
}

func (s *Scanner) publishFailure(generation uint64, started time.Time, category fault.Category) {
	previous := s.cache.Load()
	now := s.now().UTC()
	reason := "refresh failed: " + string(category)
	if previous.TotalsKnown {
		previous.RefreshGeneration = generation
		previous.LastRefreshAt = now
		previous.LastAttemptAt = started
		previous.RefreshFailure = category
		previous.Evidence.Stale = true
		previous.Evidence.State = evidence.Unknown
		previous.Evidence.Reason = reason
		for index := range previous.Evidence.Capabilities {
			if previous.Evidence.Capabilities[index].Support == evidence.Supported {
				previous.Evidence.Capabilities[index].State = evidence.Unknown
				previous.Evidence.Capabilities[index].Reason = "stale after " + reason
			}
		}
		_ = s.cache.Publish(previous)
		return
	}
	descriptor := s.format.Descriptor()
	failed := Initial(descriptor)
	failed.RefreshGeneration = generation
	failed.LastRefreshAt = now
	failed.LastAttemptAt = started
	failed.RefreshFailure = category
	failed.Evidence.Generation = generation
	failed.Evidence.StartedAt = started
	failed.Evidence.Completeness = evidence.Incomplete
	failed.Evidence.Stale = true
	failed.Evidence.Reason = reason
	for index := range failed.Evidence.Capabilities {
		if failed.Evidence.Capabilities[index].ID != evidence.ObjectInventory {
			failed.Evidence.Capabilities[index].Reason = "scan incomplete"
		}
	}
	_ = s.cache.Publish(failed)
}

type builder struct {
	descriptor     repository.Descriptor
	pagesExamined  int64
	objectCount    int64
	storedBytes    int64
	unscoped       int64
	scopes         map[string]*Scope
	recent         recentHeap
	recentLimit    int
	catalogObjects []store.Object
	barmanWAL      *barmancloud.WALCollector
}

func newBuilder(descriptor repository.Descriptor, initialScopes []string, recentLimit int, analyzeBarman bool) (*builder, error) {
	if len(initialScopes) > MaxScopes {
		return nil, &scanError{kind: fault.SafetyLimit}
	}
	builderValue := &builder{
		descriptor: descriptor, scopes: make(map[string]*Scope, len(initialScopes)), recentLimit: recentLimit,
	}
	if analyzeBarman && descriptor.ID == "barman-cloud" {
		builderValue.barmanWAL = barmancloud.NewWALCollector()
	}
	for _, name := range initialScopes {
		if invalidScopeName(name) {
			return nil, &scanError{kind: fault.Incompatible}
		}
		if _, exists := builderValue.scopes[name]; exists {
			return nil, &scanError{kind: fault.Incompatible}
		}
		builderValue.scopes[name] = &Scope{Name: name}
	}
	heap.Init(&builderValue.recent)
	return builderValue, nil
}

func (b *builder) add(object store.Object, match repository.ScopeMatch, matched bool) error {
	if object.Key == "" || len(object.Key) > store.MaxKeyBytes || object.Size < 0 || b.objectCount == math.MaxInt64 || object.Size > math.MaxInt64-b.storedBytes {
		return &scanError{kind: fault.SafetyLimit}
	}
	b.objectCount++
	b.storedBytes += object.Size
	scopeName := ""
	if matched {
		scopeName = match.Name
		if invalidScopeName(scopeName) {
			return &scanError{kind: fault.Incompatible}
		}
		scopeValue, exists := b.scopes[scopeName]
		if !exists {
			if len(b.scopes) >= MaxScopes {
				return &scanError{kind: fault.SafetyLimit}
			}
			scopeValue = &Scope{Name: scopeName}
			b.scopes[scopeName] = scopeValue
		}
		if scopeValue.ObjectCount == math.MaxInt64 || object.Size > math.MaxInt64-scopeValue.StoredBytes {
			return &scanError{kind: fault.SafetyLimit}
		}
		scopeValue.ObjectCount++
		scopeValue.StoredBytes += object.Size
		scopeValue.Recognized = scopeValue.Recognized || match.Recognized
	} else {
		b.unscoped++
	}
	b.addRecent(RecentObject{Key: object.Key, Scope: scopeName, Size: object.Size, LastModified: object.LastModified.UTC()})
	if matched {
		_, _, ok := barmanBackupObject(object.Key)
		if ok {
			if len(b.catalogObjects) >= MaxCatalogObjects {
				return &scanError{kind: fault.SafetyLimit}
			}
			b.catalogObjects = append(b.catalogObjects, object)
		}
	}
	if matched && b.barmanWAL != nil {
		b.barmanWAL.Add(object)
	}
	return nil
}

func barmanBackupObject(key string) (string, string, bool) {
	parts := strings.Split(key, "/")
	if len(parts) < 4 || parts[0] == "" || parts[1] != "base" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func applyBarmanEvidence(snapshot *Snapshot) {
	state := evidence.Healthy
	reason := "Barman backup catalog evaluated"
	if len(snapshot.BarmanCatalog.Backups) == 0 {
		state, reason = evidence.Unknown, "no Barman backup metadata observed"
	} else {
		for _, backup := range snapshot.BarmanCatalog.Backups {
			switch backup.State {
			case evidence.Unknown:
				state, reason = evidence.Unknown, "Barman catalog contains unknown backup evidence"
			case evidence.Unhealthy:
				if state != evidence.Unknown {
					state, reason = evidence.Unhealthy, "Barman catalog contains failed or incomplete backup evidence"
				}
			case evidence.Warning:
				if state == evidence.Healthy {
					state, reason = evidence.Warning, "Barman catalog contains in-progress backup evidence"
				}
			}
		}
	}
	for index := range snapshot.Evidence.Capabilities {
		capability := &snapshot.Evidence.Capabilities[index]
		if capability.ID == evidence.CatalogListing || capability.ID == evidence.StructuralValidation {
			capability.Support, capability.State, capability.Reason = evidence.Supported, state, reason
		}
	}
	snapshot.Evidence.State, snapshot.Evidence.Reason = state, reason
}

func applyBarmanWALEvidence(snapshot *Snapshot) {
	state := evidence.Unknown
	reason := "no Barman WAL scopes evaluated"
	if len(snapshot.BarmanWAL.Servers) > 0 {
		state = evidence.Healthy
		reason = "Barman WAL continuity evaluated"
		for _, server := range snapshot.BarmanWAL.Servers {
			state = conservativeState(state, server.State)
		}
		switch state {
		case evidence.Unknown:
			reason = "Barman WAL continuity contains unknown evidence"
		case evidence.Unhealthy:
			reason = "Barman WAL continuity contains confirmed gaps"
		case evidence.Warning:
			reason = "Barman WAL continuity contains candidate gaps"
		}
	}
	for index := range snapshot.Evidence.Capabilities {
		capability := &snapshot.Evidence.Capabilities[index]
		if capability.ID == evidence.WALContinuity {
			capability.Support, capability.State, capability.Reason = evidence.Supported, state, reason
		}
	}
	snapshot.Evidence.State = conservativeState(snapshot.Evidence.State, state)
	switch snapshot.Evidence.State {
	case evidence.Unknown:
		snapshot.Evidence.Reason = "Barman catalog or WAL continuity contains unknown evidence"
	case evidence.Unhealthy:
		snapshot.Evidence.Reason = "Barman catalog or WAL continuity contains unhealthy evidence"
	case evidence.Warning:
		snapshot.Evidence.Reason = "Barman catalog or WAL continuity contains warning evidence"
	default:
		snapshot.Evidence.Reason = "Barman catalog and WAL continuity evaluated"
	}
}

func conservativeState(left, right evidence.State) evidence.State {
	if left == evidence.Unknown || right == evidence.Unknown {
		return evidence.Unknown
	}
	if left == evidence.Unhealthy || right == evidence.Unhealthy {
		return evidence.Unhealthy
	}
	if left == evidence.Warning || right == evidence.Warning {
		return evidence.Warning
	}
	return evidence.Healthy
}

func applyBarmanRecoveryEvidence(snapshot *Snapshot) {
	timelineState, coverageState, retentionState := evidence.Unknown, evidence.Unknown, evidence.Unknown
	if len(snapshot.BarmanRecovery.Servers) > 0 {
		timelineState, coverageState, retentionState = evidence.Healthy, evidence.Healthy, evidence.Healthy
		for _, server := range snapshot.BarmanRecovery.Servers {
			timelineState = conservativeState(timelineState, server.TimelineState)
			coverageState = conservativeState(coverageState, server.CoverageState)
			retentionState = conservativeState(retentionState, server.Retention.State)
		}
	}
	setCapability := func(id evidence.CapabilityID, state evidence.State, reason string) {
		for index := range snapshot.Evidence.Capabilities {
			if snapshot.Evidence.Capabilities[index].ID == id {
				snapshot.Evidence.Capabilities[index].Support = evidence.Supported
				snapshot.Evidence.Capabilities[index].State = state
				snapshot.Evidence.Capabilities[index].Reason = reason
			}
		}
	}
	setCapability(evidence.TimelineTraversal, timelineState, recoveryCapabilityReason("timeline history", timelineState))
	setCapability(evidence.RecoveryCoverage, coverageState, recoveryCapabilityReason("observed recovery coverage", coverageState))
	setCapability(evidence.RetentionExpectation, retentionState, recoveryCapabilityReason("retention evidence", retentionState))
	snapshot.Evidence.State = conservativeState(snapshot.Evidence.State, conservativeState(timelineState, conservativeState(coverageState, retentionState)))
	if snapshot.Evidence.State != evidence.Healthy {
		snapshot.Evidence.Reason = "Barman catalog, WAL, timeline, recovery, or retention evidence is not healthy"
	} else {
		snapshot.Evidence.Reason = "Barman catalog, WAL, timeline, recovery, and retention evidence evaluated"
	}
}

func recoveryCapabilityReason(name string, state evidence.State) string {
	switch state {
	case evidence.Healthy:
		return name + " evaluated"
	case evidence.Warning:
		return name + " contains provisional evidence"
	case evidence.Unhealthy:
		return name + " contains definite structural failure"
	default:
		return name + " contains unknown evidence"
	}
}

func invalidScopeName(name string) bool {
	return name == "" || len(name) > 128 || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsControl) >= 0
}

func (b *builder) addRecent(object RecentObject) {
	if len(b.recent) < b.recentLimit {
		heap.Push(&b.recent, object)
		return
	}
	if moreRecent(object, b.recent[0]) {
		b.recent[0] = object
		heap.Fix(&b.recent, 0)
	}
}

func (b *builder) complete(generation uint64, started, completed time.Time) Snapshot {
	envelope := evidence.InitialSnapshot(b.descriptor.ID, b.descriptor.ScopeKind, b.descriptor.Capabilities)
	envelope.Generation = generation
	envelope.StartedAt = started
	envelope.CompletedAt = completed
	envelope.Completeness = evidence.Complete
	envelope.State = evidence.Unknown
	envelope.Reason = "backup semantics not evaluated"
	SetInventoryCapability(&envelope, evidence.Healthy, "complete object listing")
	scopes := make([]Scope, 0, len(b.scopes))
	for _, scopeValue := range b.scopes {
		scopes = append(scopes, *scopeValue)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Name < scopes[j].Name })
	recent := make([]RecentObject, len(b.recent))
	copy(recent, b.recent)
	sort.Slice(recent, func(i, j int) bool { return moreRecent(recent[i], recent[j]) })
	return Snapshot{
		Evidence: envelope, RefreshGeneration: generation, LastRefreshAt: completed, LastAttemptAt: started,
		PagesExamined: b.pagesExamined, ObjectsExamined: b.objectCount,
		TotalsKnown: true, ObjectCount: b.objectCount, StoredBytes: b.storedBytes,
		UnscopedObjectCount: b.unscoped, Scopes: scopes, RecentObjects: recent,
	}
}

type recentHeap []RecentObject

func (h recentHeap) Len() int { return len(h) }
func (h recentHeap) Less(i, j int) bool {
	return moreRecent(h[j], h[i])
}
func (h recentHeap) Swap(i, j int)   { h[i], h[j] = h[j], h[i] }
func (h *recentHeap) Push(value any) { *h = append(*h, value.(RecentObject)) }
func (h *recentHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func moreRecent(left, right RecentObject) bool {
	if !left.LastModified.Equal(right.LastModified) {
		return left.LastModified.After(right.LastModified)
	}
	return left.Key < right.Key
}

type scanError struct{ kind fault.Category }

func (e *scanError) Error() string            { return fmt.Sprintf("inventory scan failed: %s", e.kind) }
func (e *scanError) Category() fault.Category { return e.kind }
