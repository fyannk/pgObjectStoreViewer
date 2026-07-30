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

// Package pgbackrest owns pgBackRest stanza layout. Catalog, dependency, and
// archive semantics intentionally remain unimplemented until Slices 6 and 7.
package pgbackrest

import (
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/fyannk/pgObjectStoreViewer/internal/evidence"
	"github.com/fyannk/pgObjectStoreViewer/internal/repository"
)

type Format struct{}

func New() Format { return Format{} }

func (Format) Descriptor() repository.Descriptor {
	return repository.Descriptor{
		ID: "pgbackrest", DisplayName: "pgBackRest", ScopeKind: "stanza",
		Capabilities: capabilities(),
	}
}

func (Format) NewScopeMatcher(configured []string) (repository.ScopeMatcher, error) {
	set := make(map[string]struct{}, len(configured))
	for _, name := range configured {
		if !validDiscoveredName(name) {
			return nil, errors.New("pgbackrest scope matcher: invalid configured stanza")
		}
		if _, exists := set[name]; exists {
			return nil, errors.New("pgbackrest scope matcher: duplicate configured stanza")
		}
		set[name] = struct{}{}
	}
	return &matcher{configured: slices.Clone(configured), configuredSet: set}, nil
}

type matcher struct {
	configured    []string
	configuredSet map[string]struct{}
}

func (m *matcher) InitialScopes() []string { return slices.Clone(m.configured) }

func (m *matcher) Match(key string) (repository.ScopeMatch, bool) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 || parts[2] == "" || (parts[0] != "backup" && parts[0] != "archive") || !validDiscoveredName(parts[1]) {
		return repository.ScopeMatch{}, false
	}
	if len(m.configured) > 0 {
		if _, ok := m.configuredSet[parts[1]]; !ok {
			return repository.ScopeMatch{}, false
		}
	}
	return repository.ScopeMatch{Name: parts[1], Recognized: true}, true
}

func capabilities() []evidence.CapabilityID {
	return []evidence.CapabilityID{
		evidence.ObjectInventory,
		evidence.CatalogListing,
		evidence.StructuralValidation,
		evidence.DependencyValidation,
		evidence.WALContinuity,
		evidence.TimelineTraversal,
		evidence.EncryptedMetadata,
		evidence.RecoveryCoverage,
		evidence.RetentionExpectation,
	}
}

func validDiscoveredName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 128 &&
		!strings.ContainsAny(name, "/\\") && strings.IndexFunc(name, unicode.IsControl) < 0
}

var _ repository.Format = Format{}
