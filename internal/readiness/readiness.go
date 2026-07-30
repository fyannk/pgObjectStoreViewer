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

// Package readiness tracks only configuration validity and a lightweight,
// recent store reachability result. It never runs a catalog scan.
package readiness

import (
	"sync"
	"time"

	"github.com/fyannk/objectstoreviewer/internal/fault"
)

// Clock makes freshness deterministic in tests.
type Clock func() time.Time

// Result is safe for health endpoints and contains no store topology.
type Result struct {
	Ready     bool
	Category  fault.Category
	CheckedAt time.Time
}

type ProbeState struct {
	mu          sync.RWMutex
	validConfig bool
	maxAge      time.Duration
	now         Clock
	reachable   bool
	checkedAt   time.Time
	category    fault.Category
}

func New(validConfig bool, maxAge time.Duration, now Clock) *ProbeState {
	if now == nil {
		now = time.Now
	}
	return &ProbeState{validConfig: validConfig, maxAge: maxAge, now: now, category: fault.Unavailable}
}

func (s *ProbeState) MarkReachable(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reachable = true
	s.checkedAt = at.UTC()
	s.category = fault.Unknown
}

func (s *ProbeState) MarkFailure(at time.Time, category fault.Category) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reachable = false
	s.checkedAt = at.UTC()
	s.category = category
}

func (s *ProbeState) Result() Result {
	s.mu.RLock()
	validConfig := s.validConfig
	maxAge := s.maxAge
	reachable := s.reachable
	checkedAt := s.checkedAt
	category := s.category
	s.mu.RUnlock()

	if !validConfig {
		return Result{Category: fault.InvalidConfig}
	}
	if checkedAt.IsZero() || !reachable {
		return Result{Category: category, CheckedAt: checkedAt}
	}
	if maxAge <= 0 || s.now().UTC().Sub(checkedAt) > maxAge {
		return Result{Category: fault.Unavailable, CheckedAt: checkedAt}
	}
	return Result{Ready: true, Category: fault.Unknown, CheckedAt: checkedAt}
}
