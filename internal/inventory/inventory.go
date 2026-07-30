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

// Package inventory owns immutable provider-neutral object inventory
// snapshots. It does not infer backup or WAL semantics.
package inventory

import (
	"errors"
	"math"
	"slices"
	"sync/atomic"
	"time"

	"github.com/fyannk/objectstoreviewer/internal/evidence"
	"github.com/fyannk/objectstoreviewer/internal/fault"
	"github.com/fyannk/objectstoreviewer/internal/formats/barmancloud"
	"github.com/fyannk/objectstoreviewer/internal/repository"
	"github.com/fyannk/objectstoreviewer/internal/store"
)

const (
	MaxScopes         = 1_000
	MaxRecentObjects  = 200
	MaxCatalogObjects = 10_000
)

type Scope struct {
	Name        string
	Recognized  bool
	ObjectCount int64
	StoredBytes int64
}

type RecentObject struct {
	Key          string
	Scope        string
	Size         int64
	LastModified time.Time
}

// Snapshot separates totals from attempt diagnostics so incomplete evidence
// cannot accidentally render partial counters as complete totals.
type Snapshot struct {
	Evidence            evidence.Snapshot
	RefreshGeneration   uint64
	LastRefreshAt       time.Time
	LastAttemptAt       time.Time
	RefreshFailure      fault.Category
	PagesExamined       int64
	ObjectsExamined     int64
	TotalsKnown         bool
	ObjectCount         int64
	StoredBytes         int64
	UnscopedObjectCount int64
	Scopes              []Scope
	RecentObjects       []RecentObject
	BarmanCatalog       barmancloud.Catalog
	BarmanWAL           barmancloud.WALCatalog
	BarmanRecovery      barmancloud.RecoveryCatalog
}

func Initial(format repository.Descriptor) Snapshot {
	envelope := evidence.InitialSnapshot(format.ID, format.ScopeKind, format.Capabilities)
	setInventoryCapability(&envelope, evidence.Unknown, "no completed scan")
	return Snapshot{Evidence: envelope}
}

func (s Snapshot) Validate() error {
	if err := s.Evidence.Validate(); err != nil {
		return err
	}
	if len(s.Scopes) > MaxScopes || len(s.RecentObjects) > MaxRecentObjects || len(s.BarmanCatalog.Backups) > MaxCatalogObjects {
		return errors.New("inventory snapshot exceeds a safety limit")
	}
	if err := s.BarmanWAL.Validate(); err != nil {
		return err
	}
	if err := s.BarmanRecovery.Validate(); err != nil {
		return err
	}
	if s.PagesExamined < 0 || s.ObjectsExamined < 0 || s.ObjectCount < 0 || s.StoredBytes < 0 || s.UnscopedObjectCount < 0 {
		return errors.New("inventory snapshot contains a negative counter")
	}
	if s.RefreshFailure != "" && !validFailureCategory(s.RefreshFailure) {
		return errors.New("inventory snapshot contains an invalid refresh failure")
	}
	if (!s.LastRefreshAt.IsZero() && s.LastRefreshAt.Location() != time.UTC) || (!s.LastAttemptAt.IsZero() && s.LastAttemptAt.Location() != time.UTC) || (!s.LastAttemptAt.IsZero() && !s.LastRefreshAt.IsZero() && s.LastRefreshAt.Before(s.LastAttemptAt)) {
		return errors.New("inventory snapshot contains invalid refresh timestamps")
	}
	if !s.TotalsKnown && (s.ObjectCount != 0 || s.StoredBytes != 0 || s.UnscopedObjectCount != 0 || len(s.Scopes) != 0 || len(s.RecentObjects) != 0) {
		return errors.New("unknown totals contain partial inventory")
	}
	if s.TotalsKnown && s.Evidence.Completeness != evidence.Complete {
		return errors.New("known totals require complete evidence")
	}
	scopedObjects := int64(0)
	scopedBytes := int64(0)
	for index, scope := range s.Scopes {
		if scope.Name == "" || scope.ObjectCount < 0 || scope.StoredBytes < 0 {
			return errors.New("invalid scope inventory")
		}
		if index > 0 && s.Scopes[index-1].Name >= scope.Name {
			return errors.New("scope inventory is not uniquely sorted")
		}
		if scope.ObjectCount > math.MaxInt64-scopedObjects || scope.StoredBytes > math.MaxInt64-scopedBytes {
			return errors.New("scope inventory counters overflow")
		}
		scopedObjects += scope.ObjectCount
		scopedBytes += scope.StoredBytes
	}
	if s.TotalsKnown && (scopedObjects > s.ObjectCount || s.UnscopedObjectCount != s.ObjectCount-scopedObjects || scopedBytes > s.StoredBytes) {
		return errors.New("scope inventory does not reconcile with totals")
	}
	for _, object := range s.RecentObjects {
		if object.Key == "" || len(object.Key) > store.MaxKeyBytes || object.Size < 0 {
			return errors.New("invalid recent object")
		}
	}
	if s.TotalsKnown && int64(len(s.RecentObjects)) > s.ObjectCount {
		return errors.New("recent inventory exceeds object total")
	}
	return nil
}

func validFailureCategory(category fault.Category) bool {
	switch category {
	case fault.Canceled, fault.Timeout, fault.InvalidConfig, fault.Authentication, fault.Authorization,
		fault.Throttled, fault.Unavailable, fault.NotFound, fault.Incompatible, fault.SafetyLimit:
		return true
	default:
		return false
	}
}

// Cache atomically publishes deep-cloned snapshots.
type Cache struct {
	value atomic.Pointer[Snapshot]
}

func NewCache(initial Snapshot) (*Cache, error) {
	cache := &Cache{}
	if err := cache.Publish(initial); err != nil {
		return nil, err
	}
	return cache, nil
}

func (c *Cache) Load() Snapshot {
	current := c.value.Load()
	if current == nil {
		return Snapshot{}
	}
	return clone(*current)
}

func (c *Cache) Publish(snapshot Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	copyValue := clone(snapshot)
	c.value.Store(&copyValue)
	return nil
}

func clone(snapshot Snapshot) Snapshot {
	snapshot.Evidence.Capabilities = slices.Clone(snapshot.Evidence.Capabilities)
	snapshot.Scopes = slices.Clone(snapshot.Scopes)
	snapshot.RecentObjects = slices.Clone(snapshot.RecentObjects)
	snapshot.BarmanCatalog.Backups = slices.Clone(snapshot.BarmanCatalog.Backups)
	snapshot.BarmanWAL = barmancloud.CloneWALCatalog(snapshot.BarmanWAL)
	snapshot.BarmanRecovery = barmancloud.CloneRecoveryCatalog(snapshot.BarmanRecovery)
	return snapshot
}

func setInventoryCapability(envelope *evidence.Snapshot, state evidence.State, reason string) {
	for index := range envelope.Capabilities {
		if envelope.Capabilities[index].ID == evidence.ObjectInventory {
			envelope.Capabilities[index].Support = evidence.Supported
			envelope.Capabilities[index].State = state
			envelope.Capabilities[index].Reason = reason
		}
	}
}

func SetInventoryCapability(envelope *evidence.Snapshot, state evidence.State, reason string) {
	setInventoryCapability(envelope, state, reason)
}
