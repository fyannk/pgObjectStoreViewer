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

// Package fault defines the stable, non-sensitive failure vocabulary exposed
// across provider, format, logging, and HTTP boundaries.
package fault

import (
	"context"
	"errors"
)

// Category is deliberately small and never includes an upstream message.
type Category string

const (
	Unknown        Category = "unknown"
	Canceled       Category = "canceled"
	Timeout        Category = "timeout"
	InvalidConfig  Category = "invalid_configuration"
	Authentication Category = "authentication"
	Authorization  Category = "authorization"
	Throttled      Category = "throttled"
	Unavailable    Category = "unavailable"
	NotFound       Category = "not_found"
	Incompatible   Category = "incompatible_format"
	SafetyLimit    Category = "safety_limit"
)

// Categorize maps only errors whose meaning can be established safely here.
// Provider adapters add typed mappings without exposing SDK error strings.
func Categorize(err error) Category {
	var categorized interface{ Category() Category }
	switch {
	case err == nil:
		return Unknown
	case errors.Is(err, context.Canceled):
		return Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return Timeout
	case errors.As(err, &categorized):
		return categorized.Category()
	default:
		return Unknown
	}
}
