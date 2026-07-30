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

// Package v1alpha1 contains the provider-SDK-free ObjectStoreViewer evidence
// wire contract. It is safe for producers, consumers, and deployment tooling
// to import without gaining object-store credentials or repository parsers.
package v1alpha1

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	APIVersion             = "evidence.objectstoreviewer.io/v1alpha1"
	SnapshotKind           = "RepositoryEvidenceSnapshot"
	BarmanDetailsType      = "barman-cloud/v1alpha1"
	BarmanBackupPageKind   = "BarmanBackupPage"
	BarmanWALRangePageKind = "BarmanWALRangePage"
	BarmanWALGapPageKind   = "BarmanWALGapPage"
	BarmanRecoveryPageKind = "BarmanRecoveryPathPage"
	ErrorKind              = "EvidenceAPIError"
	HealthKind             = "HealthStatus"
	ReadinessKind          = "ReadinessStatus"
	HealthLive             = "live"
	ReadinessReady         = "ready"
	ReadinessNotReady      = "not-ready"
	ProducerName           = "objectstoreviewer"
)

// State is the normative four-state evidence vocabulary.
type State string

const (
	Healthy   State = "healthy"
	Warning   State = "warning"
	Unhealthy State = "unhealthy"
	Unknown   State = "unknown"
)

// Support states whether the selected repository format proves a capability.
type Support string

const (
	Supported      Support = "supported"
	Unsupported    Support = "unsupported"
	SupportUnknown Support = "unknown"
)

// Completeness records whether all required evidence was collected.
type Completeness string

const (
	Complete   Completeness = "complete"
	Incomplete Completeness = "incomplete"
	Unscanned  Completeness = "no-completed-scan"
)

// CapabilityID identifies a format-neutral evidence operation.
type CapabilityID string

const (
	ObjectInventory      CapabilityID = "object-inventory"
	CatalogListing       CapabilityID = "catalog-listing"
	StructuralValidation CapabilityID = "structural-artifact-validation"
	DependencyValidation CapabilityID = "dependency-chain-validation"
	WALContinuity        CapabilityID = "wal-continuity"
	TimelineTraversal    CapabilityID = "timeline-history-traversal"
	EncryptedMetadata    CapabilityID = "encrypted-metadata"
	RecoveryCoverage     CapabilityID = "observed-recovery-coverage"
	RetentionExpectation CapabilityID = "retention-expectation-comparison"
)

var capabilityIDs = [...]CapabilityID{
	CatalogListing,
	DependencyValidation,
	EncryptedMetadata,
	ObjectInventory,
	RecoveryCoverage,
	RetentionExpectation,
	StructuralValidation,
	TimelineTraversal,
	WALContinuity,
}

// FailureCategory is the stable, redacted refresh failure vocabulary.
type FailureCategory string

const (
	FailureCanceled           FailureCategory = "canceled"
	FailureTimeout            FailureCategory = "timeout"
	FailureInvalidConfig      FailureCategory = "invalid_configuration"
	FailureAuthentication     FailureCategory = "authentication"
	FailureAuthorization      FailureCategory = "authorization"
	FailureThrottled          FailureCategory = "throttled"
	FailureUnavailable        FailureCategory = "unavailable"
	FailureNotFound           FailureCategory = "not_found"
	FailureIncompatibleFormat FailureCategory = "incompatible_format"
	FailureSafetyLimit        FailureCategory = "safety_limit"
)

// Reason separates a stable machine code from bounded operator context.
type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// StateReason is a complete semantic result.
type StateReason struct {
	State  State  `json:"state"`
	Reason Reason `json:"reason"`
}

// Capability preserves implementation support and current evidence.
type Capability struct {
	ID      CapabilityID `json:"id"`
	Support Support      `json:"support"`
	State   State        `json:"state"`
	Reason  Reason       `json:"reason"`
}

// Producer identifies the emitting ObjectStoreViewer build.
type Producer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClusterIdentity is operator-injected correlation input. Name is display-only.
type ClusterIdentity struct {
	Namespace string  `json:"namespace"`
	UID       string  `json:"uid"`
	Name      *string `json:"name"`
}

// ScopeIdentity identifies exactly one format-owned scope.
type ScopeIdentity struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// RepositoryIdentity identifies a credential-free repository destination.
type RepositoryIdentity struct {
	Provider               string        `json:"provider"`
	Format                 string        `json:"format"`
	DestinationFingerprint string        `json:"destination_fingerprint"`
	Scope                  ScopeIdentity `json:"scope"`
}

// Identity binds repository evidence to one immutable CNPG Cluster identity.
type Identity struct {
	Cluster    ClusterIdentity    `json:"cluster"`
	Repository RepositoryIdentity `json:"repository"`
}

// InventorySummary contains only provider-neutral, allowlisted counts.
type InventorySummary struct {
	Known               bool             `json:"known"`
	ObjectCount         *uint64          `json:"object_count"`
	StoredBytes         *uint64          `json:"stored_bytes"`
	UnscopedObjectCount *uint64          `json:"unscoped_object_count"`
	PagesExamined       uint64           `json:"pages_examined"`
	ObjectsExamined     uint64           `json:"objects_examined"`
	LastFailureCategory *FailureCategory `json:"last_failure_category"`
}

// StateCounts counts every normative evidence state.
type StateCounts struct {
	Healthy   uint64 `json:"healthy"`
	Warning   uint64 `json:"warning"`
	Unhealthy uint64 `json:"unhealthy"`
	Unknown   uint64 `json:"unknown"`
}

// BarmanWALCounts counts every supported Barman WAL object class.
type BarmanWALCounts struct {
	Segment       uint64 `json:"segment"`
	Partial       uint64 `json:"partial"`
	History       uint64 `json:"history"`
	BackupHistory uint64 `json:"backup_history"`
	Unknown       uint64 `json:"unknown"`
	Duplicate     uint64 `json:"duplicate"`
}

// BarmanRetentionSummary is descriptive unless an expectation is configured.
type BarmanRetentionSummary struct {
	VisibleBackups            *uint64    `json:"visible_backups"`
	StructurallyUsableBackups *uint64    `json:"structurally_usable_backups"`
	OldestCompletionAt        *time.Time `json:"oldest_completion_at"`
	NewestCompletionAt        *time.Time `json:"newest_completion_at"`
	MinimumConfigured         bool       `json:"minimum_configured"`
	MinimumRedundancy         *uint64    `json:"minimum_redundancy"`
	PolicyConfigured          bool       `json:"policy_configured"`
	State                     State      `json:"state"`
	Reason                    Reason     `json:"reason"`
}

// BarmanCloudSummary is the bounded format-owned snapshot summary.
type BarmanCloudSummary struct {
	BackupItems               *uint64                `json:"backup_items"`
	WALRangeItems             *uint64                `json:"wal_range_items"`
	WALGapItems               *uint64                `json:"wal_gap_items"`
	RecoveryPathItems         *uint64                `json:"recovery_path_items"`
	StructurallyUsableBackups *uint64                `json:"structurally_usable_backups"`
	BackupStates              *StateCounts           `json:"backup_states"`
	WALCounts                 *BarmanWALCounts       `json:"wal_counts"`
	WAL                       StateReason            `json:"wal"`
	Timeline                  StateReason            `json:"timeline"`
	Coverage                  StateReason            `json:"coverage"`
	Retention                 BarmanRetentionSummary `json:"retention"`
	LatestArchiveReceiptAt    *time.Time             `json:"latest_archive_receipt_at"`
	RangesTruncated           bool                   `json:"ranges_truncated"`
	DiagnosticsTruncated      bool                   `json:"diagnostics_truncated"`
}

// Details is a closed tagged union. Unknown tags retain only the bounded tag;
// their payload is discarded so arbitrary data cannot cross the boundary.
type Details struct {
	Type        string              `json:"type"`
	BarmanCloud *BarmanCloudSummary `json:"barman_cloud,omitempty"`
	unknown     bool
}

// Unknown reports whether an unrecognized details tag was retained.
func (d Details) Unknown() bool { return d.unknown }

// MarshalJSON emits only a recognized payload or the retained unknown tag.
func (d Details) MarshalJSON() ([]byte, error) {
	if d.unknown || d.Type != BarmanDetailsType {
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: d.Type})
	}
	return json.Marshal(struct {
		Type        string              `json:"type"`
		BarmanCloud *BarmanCloudSummary `json:"barman_cloud"`
	}{Type: d.Type, BarmanCloud: d.BarmanCloud})
}

// UnmarshalJSON validates the tag before retaining any format-owned payload.
func (d *Details) UnmarshalJSON(data []byte) error {
	var tagged struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &tagged); err != nil {
		return err
	}
	if invalidText(tagged.Type, 64) {
		return errors.New("details type must be bounded")
	}
	d.Type, d.BarmanCloud, d.unknown = tagged.Type, nil, false
	if tagged.Type != BarmanDetailsType {
		d.unknown = true
		return nil
	}
	var known struct {
		BarmanCloud *BarmanCloudSummary `json:"barman_cloud"`
	}
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	d.BarmanCloud = known.BarmanCloud
	return nil
}

// RepositoryEvidenceSnapshot is the complete immutable API publication.
type RepositoryEvidenceSnapshot struct {
	APIVersion         string           `json:"api_version"`
	Kind               string           `json:"kind"`
	Producer           Producer         `json:"producer"`
	Identity           Identity         `json:"identity"`
	Revision           uint64           `json:"revision"`
	EvidenceGeneration uint64           `json:"evidence_generation"`
	StartedAt          *time.Time       `json:"started_at"`
	CompletedAt        *time.Time       `json:"completed_at"`
	LastAttemptAt      *time.Time       `json:"last_attempt_at"`
	Completeness       Completeness     `json:"completeness"`
	Stale              bool             `json:"stale"`
	State              State            `json:"state"`
	Reason             Reason           `json:"reason"`
	Capabilities       []Capability     `json:"capabilities"`
	Inventory          InventorySummary `json:"inventory"`
	Details            Details          `json:"details"`
}

// BarmanBackup is one allowlisted Barman structural-evidence item.
type BarmanBackup struct {
	Server              string     `json:"server"`
	BackupID            string     `json:"backup_id"`
	Status              *string    `json:"status"`
	BackupType          *string    `json:"backup_type"`
	State               State      `json:"state"`
	Reason              Reason     `json:"reason"`
	SystemID            *string    `json:"system_id"`
	PostgreSQLVersion   *uint64    `json:"postgresql_version"`
	Timeline            *uint64    `json:"timeline"`
	WALSegmentSizeBytes *uint64    `json:"wal_segment_size_bytes"`
	BeginWAL            *string    `json:"begin_wal"`
	EndWAL              *string    `json:"end_wal"`
	BeginLSN            *string    `json:"begin_lsn"`
	EndLSN              *string    `json:"end_lsn"`
	BeginAt             *time.Time `json:"begin_at"`
	EndAt               *time.Time `json:"end_at"`
	LogicalBytes        *uint64    `json:"logical_bytes"`
	DeduplicatedBytes   *uint64    `json:"deduplicated_bytes"`
	StoredArtifactBytes *uint64    `json:"stored_artifact_bytes"`
	Compression         *string    `json:"compression"`
	Encryption          *string    `json:"encryption"`
	ArtifactCount       *uint64    `json:"artifact_count"`
	TablespaceCount     *uint64    `json:"tablespace_count"`
}

// BarmanWALRange is one compact, contiguous WAL segment range.
type BarmanWALRange struct {
	Server          string     `json:"server"`
	Timeline        uint64     `json:"timeline"`
	StartPosition   uint64     `json:"start_position"`
	EndPosition     uint64     `json:"end_position"`
	SegmentCount    uint64     `json:"segment_count"`
	FirstWAL        string     `json:"first_wal"`
	LastWAL         string     `json:"last_wal"`
	LatestReceiptAt *time.Time `json:"latest_receipt_at"`
	EndReceiptAt    *time.Time `json:"end_receipt_at"`
}

// GapStatus records the two-complete-scan WAL-gap lifecycle.
type GapStatus string

const (
	GapCandidate GapStatus = "candidate"
	GapConfirmed GapStatus = "confirmed"
)

// BarmanWALGap is one candidate or confirmed missing WAL range.
type BarmanWALGap struct {
	Server                  string    `json:"server"`
	Timeline                uint64    `json:"timeline"`
	StartPosition           uint64    `json:"start_position"`
	EndPosition             uint64    `json:"end_position"`
	SegmentCount            uint64    `json:"segment_count"`
	FirstWAL                string    `json:"first_wal"`
	LastWAL                 string    `json:"last_wal"`
	Status                  GapStatus `json:"status"`
	FirstObservedGeneration uint64    `json:"first_observed_generation"`
	LastObservedGeneration  uint64    `json:"last_observed_generation"`
}

// CoverageStop identifies why an observed recovery path ends.
type CoverageStop string

const (
	CoverageFrontier         CoverageStop = "frontier"
	CoverageCandidateLimited CoverageStop = "candidate-limited"
	CoverageGapLimited       CoverageStop = "gap-limited"
	CoverageUnknownLimited   CoverageStop = "unknown-limited"
)

// BarmanRecoveryPath is conservative observed recovery coverage from a backup.
type BarmanRecoveryPath struct {
	Server            string       `json:"server"`
	BackupID          string       `json:"backup_id"`
	TargetTimeline    uint64       `json:"target_timeline"`
	State             State        `json:"state"`
	Reason            Reason       `json:"reason"`
	Stop              CoverageStop `json:"stop"`
	LowerBoundAt      *time.Time   `json:"lower_bound_at"`
	StartTimeline     *uint64      `json:"start_timeline"`
	StartPosition     *uint64      `json:"start_position"`
	StartWAL          *string      `json:"start_wal"`
	FrontierTimeline  *uint64      `json:"frontier_timeline"`
	FrontierPosition  *uint64      `json:"frontier_position"`
	FrontierWAL       *string      `json:"frontier_wal"`
	FrontierReceiptAt *time.Time   `json:"frontier_receipt_at"`
	AssumptionCodes   []string     `json:"assumption_codes"`
}

// PageHeader is the common generation-consistent collection envelope.
type PageHeader struct {
	APIVersion         string  `json:"api_version"`
	Kind               string  `json:"kind"`
	Revision           uint64  `json:"revision"`
	EvidenceGeneration uint64  `json:"evidence_generation"`
	TotalItems         *uint64 `json:"total_items"`
	NextCursor         *string `json:"next_cursor"`
}

// BarmanBackupPage carries one bounded generation-consistent backup page.
type BarmanBackupPage struct {
	PageHeader
	Items []BarmanBackup `json:"items"`
}

// BarmanWALRangePage carries one bounded generation-consistent WAL-range page.
type BarmanWALRangePage struct {
	PageHeader
	Items []BarmanWALRange `json:"items"`
}

// BarmanWALGapPage carries one bounded generation-consistent WAL-gap page.
type BarmanWALGapPage struct {
	PageHeader
	Items []BarmanWALGap `json:"items"`
}

// BarmanRecoveryPathPage carries one bounded recovery-path page.
type BarmanRecoveryPathPage struct {
	PageHeader
	Items []BarmanRecoveryPath `json:"items"`
}

// ErrorCode is the stable API failure vocabulary.
type ErrorCode string

const (
	ErrorInvalidRequest     ErrorCode = "invalid-request"
	ErrorUnauthenticated    ErrorCode = "unauthenticated"
	ErrorNotFound           ErrorCode = "not-found"
	ErrorMethodNotAllowed   ErrorCode = "method-not-allowed"
	ErrorPublicationChanged ErrorCode = "publication-changed"
	ErrorResponseLimit      ErrorCode = "response-limit"
	ErrorBusy               ErrorCode = "busy"
	ErrorInvalidPublication ErrorCode = "invalid-publication"
)

// EvidenceAPIError is the bounded, provider-detail-free error response.
type EvidenceAPIError struct {
	APIVersion string    `json:"api_version"`
	Kind       string    `json:"kind"`
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
}

// ServiceStatus is the bounded response shared by sidecar health and
// readiness endpoints. It deliberately contains no repository identity or
// provider diagnostics.
type ServiceStatus struct {
	APIVersion string `json:"api_version"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
}

// Validate checks the closed health/readiness response vocabulary.
func (s ServiceStatus) Validate() error {
	valid := s.Kind == HealthKind && s.Status == HealthLive ||
		s.Kind == ReadinessKind && (s.Status == ReadinessReady || s.Status == ReadinessNotReady)
	if s.APIVersion != APIVersion || !valid {
		return errors.New("service status is invalid")
	}
	return nil
}

// Validate rejects unknown error codes and unbounded messages.
func (e EvidenceAPIError) Validate() error {
	if e.APIVersion != APIVersion || e.Kind != ErrorKind || !slices.Contains([]ErrorCode{ErrorInvalidRequest, ErrorUnauthenticated, ErrorNotFound, ErrorMethodNotAllowed, ErrorPublicationChanged, ErrorResponseLimit, ErrorBusy, ErrorInvalidPublication}, e.Code) || invalidText(e.Message, 256) {
		return errors.New("evidence API error is invalid")
	}
	return nil
}

// Validate checks the backup page envelope, bounds, items, and contract order.
func (p BarmanBackupPage) Validate() error {
	if err := p.PageHeader.validate(BarmanBackupPageKind, len(p.Items)); err != nil {
		return err
	}
	if p.Items == nil {
		return errors.New("backup page items must be non-null")
	}
	for index, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if index > 0 && compareBackup(p.Items[index-1], item) >= 0 {
			return errors.New("backup page items must be uniquely sorted")
		}
	}
	return nil
}

// Validate checks the WAL-range page envelope, bounds, items, and order.
func (p BarmanWALRangePage) Validate() error {
	if err := p.PageHeader.validate(BarmanWALRangePageKind, len(p.Items)); err != nil {
		return err
	}
	if p.Items == nil {
		return errors.New("WAL range page items must be non-null")
	}
	for index, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if index > 0 && compareRange(p.Items[index-1], item) >= 0 {
			return errors.New("WAL range page items must be uniquely sorted")
		}
	}
	return nil
}

// Validate checks the WAL-gap page envelope, bounds, items, and order.
func (p BarmanWALGapPage) Validate() error {
	if err := p.PageHeader.validate(BarmanWALGapPageKind, len(p.Items)); err != nil {
		return err
	}
	if p.Items == nil {
		return errors.New("WAL gap page items must be non-null")
	}
	for index, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if index > 0 && compareGap(p.Items[index-1], item) >= 0 {
			return errors.New("WAL gap page items must be uniquely sorted")
		}
	}
	return nil
}

// Validate checks the recovery-path page envelope, bounds, items, and order.
func (p BarmanRecoveryPathPage) Validate() error {
	if err := p.PageHeader.validate(BarmanRecoveryPageKind, len(p.Items)); err != nil {
		return err
	}
	if p.Items == nil {
		return errors.New("recovery path page items must be non-null")
	}
	for index, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if index > 0 && compareRecoveryPath(p.Items[index-1], item) >= 0 {
			return errors.New("recovery path page items must be uniquely sorted")
		}
	}
	return nil
}

// Validate rejects publications that could turn missing or invalid facts into
// healthy evidence. Unknown detail tags remain valid but unavailable.
func (s RepositoryEvidenceSnapshot) Validate() error {
	if s.APIVersion != APIVersion || s.Kind != SnapshotKind {
		return errors.New("snapshot has an incompatible version or kind")
	}
	if s.Producer.Name != ProducerName || invalidText(s.Producer.Version, 64) {
		return errors.New("snapshot producer is invalid")
	}
	if err := s.Identity.validate(); err != nil {
		return err
	}
	if !validState(s.State) || !validCompleteness(s.Completeness) || invalidReason(s.Reason) {
		return errors.New("snapshot evidence state is invalid")
	}
	if s.Revision < s.EvidenceGeneration {
		return errors.New("snapshot revision precedes its evidence generation")
	}
	if s.Completeness != Complete && s.State == Healthy {
		return errors.New("incomplete evidence cannot be healthy")
	}
	if s.Stale && s.State != Unknown {
		return errors.New("stale evidence must be unknown")
	}
	if s.EvidenceGeneration == 0 {
		if s.StartedAt != nil || s.CompletedAt != nil || (s.Completeness != Unscanned && s.Completeness != Incomplete) || s.State != Unknown {
			return errors.New("snapshot without a generation must remain unknown")
		}
	} else if s.Completeness != Complete || !orderedTimes(s.StartedAt, s.CompletedAt) {
		return errors.New("snapshot generation timestamps are invalid")
	}
	if !validOptionalTime(s.LastAttemptAt) {
		return errors.New("snapshot attempt timestamp is invalid")
	}
	if len(s.Capabilities) != len(capabilityIDs) {
		return errors.New("snapshot must carry the complete capability set")
	}
	for index, capability := range s.Capabilities {
		if err := capability.validate(); err != nil {
			return err
		}
		if capability.ID != capabilityIDs[index] {
			return errors.New("snapshot capabilities must be complete and sorted")
		}
		if s.Stale && capability.Support == Supported && capability.State != Unknown {
			return errors.New("stale supported capabilities must be unknown")
		}
	}
	if err := s.Inventory.validate(); err != nil {
		return err
	}
	if s.Inventory.LastFailureCategory != nil && (!s.Stale || s.State != Unknown) {
		return errors.New("a failed latest attempt must publish stale unknown evidence")
	}
	if err := s.Details.validate(); err != nil {
		return err
	}
	if s.Completeness != Complete && s.Details.BarmanCloud != nil && s.Details.BarmanCloud.hasGenerationCounts() {
		return errors.New("incomplete evidence cannot expose generation collection counts")
	}
	return nil
}

func (i Identity) validate() error {
	if invalidText(i.Cluster.Namespace, 63) || invalidText(i.Cluster.UID, 128) || invalidOptionalText(i.Cluster.Name, 253) {
		return errors.New("cluster identity is invalid")
	}
	if i.Repository.Provider != "s3" || i.Repository.Format != "barman-cloud" || i.Repository.Scope.Kind != "barman-server" || !validBarmanServer(i.Repository.Scope.Name) {
		return errors.New("repository identity is outside the initial profile")
	}
	if !validFingerprint(i.Repository.DestinationFingerprint) {
		return errors.New("repository destination fingerprint is invalid")
	}
	return nil
}

func (c Capability) validate() error {
	if !validCapability(c.ID) || !validSupport(c.Support) || !validState(c.State) || invalidReason(c.Reason) {
		return errors.New("capability is invalid")
	}
	if c.Support != Supported && c.State != Unknown {
		return fmt.Errorf("capability %s must be unknown without proven support", c.ID)
	}
	return nil
}

func (i InventorySummary) validate() error {
	if i.Known != (i.ObjectCount != nil && i.StoredBytes != nil && i.UnscopedObjectCount != nil) {
		return errors.New("inventory known flag and nullable totals disagree")
	}
	if i.LastFailureCategory != nil && !validFailure(*i.LastFailureCategory) {
		return errors.New("inventory failure category is invalid")
	}
	return nil
}

func (d Details) validate() error {
	if invalidText(d.Type, 64) {
		return errors.New("details type must be bounded")
	}
	if d.unknown || d.Type != BarmanDetailsType {
		if d.BarmanCloud != nil {
			return errors.New("unknown details cannot retain a payload")
		}
		return nil
	}
	if d.BarmanCloud == nil {
		return errors.New("Barman details payload is required")
	}
	for _, result := range []StateReason{d.BarmanCloud.WAL, d.BarmanCloud.Timeline, d.BarmanCloud.Coverage} {
		if !validState(result.State) || invalidReason(result.Reason) {
			return errors.New("Barman details state is invalid")
		}
	}
	retention := d.BarmanCloud.Retention
	if !validState(retention.State) || invalidReason(retention.Reason) || retention.MinimumConfigured != (retention.MinimumRedundancy != nil) || !validOptionalTime(retention.OldestCompletionAt) || !validOptionalTime(retention.NewestCompletionAt) || !validOptionalTime(d.BarmanCloud.LatestArchiveReceiptAt) {
		return errors.New("Barman retention details are invalid")
	}
	if retention.OldestCompletionAt != nil && retention.NewestCompletionAt != nil && retention.NewestCompletionAt.Before(*retention.OldestCompletionAt) {
		return errors.New("Barman retention completion times are unordered")
	}
	if d.BarmanCloud.RangesTruncated || d.BarmanCloud.DiagnosticsTruncated {
		if d.BarmanCloud.WAL.State != Unknown {
			return errors.New("truncated Barman WAL evidence must be unknown")
		}
	}
	if d.BarmanCloud.BackupItems != nil && d.BarmanCloud.BackupStates != nil {
		states := d.BarmanCloud.BackupStates
		if states.Healthy+states.Warning+states.Unhealthy+states.Unknown != *d.BarmanCloud.BackupItems {
			return errors.New("Barman backup state counts do not reconcile")
		}
	}
	if d.BarmanCloud.BackupItems != nil && d.BarmanCloud.StructurallyUsableBackups != nil && *d.BarmanCloud.StructurallyUsableBackups > *d.BarmanCloud.BackupItems {
		return errors.New("structurally usable backups exceed visible backups")
	}
	return nil
}

func (s BarmanCloudSummary) hasGenerationCounts() bool {
	return s.BackupItems != nil || s.WALRangeItems != nil || s.WALGapItems != nil || s.RecoveryPathItems != nil || s.StructurallyUsableBackups != nil || s.BackupStates != nil || s.WALCounts != nil
}

func (h PageHeader) validate(kind string, itemCount int) error {
	if h.APIVersion != APIVersion || h.Kind != kind {
		return errors.New("page has an incompatible version or kind")
	}
	// #nosec G115 -- itemCount is a slice length and this same condition rejects
	// anything above the 200-item page ceiling.
	if h.Revision == 0 || h.EvidenceGeneration == 0 || h.Revision < h.EvidenceGeneration || itemCount > 200 || (h.TotalItems != nil && *h.TotalItems < uint64(itemCount)) {
		return errors.New("page item bounds are invalid")
	}
	if h.NextCursor != nil && invalidText(*h.NextCursor, 4096) {
		return errors.New("page cursor is invalid")
	}
	return nil
}

// Validate checks one backup item without interpreting repository metadata.
func (b BarmanBackup) Validate() error {
	if !validBarmanServer(b.Server) || invalidText(b.BackupID, 256) || !validState(b.State) || invalidReason(b.Reason) {
		return errors.New("Barman backup identity or state is invalid")
	}
	for _, field := range []struct {
		value   *string
		maximum int
	}{{b.Status, 64}, {b.BackupType, 64}, {b.SystemID, 64}, {b.BeginWAL, 64}, {b.EndWAL, 64}, {b.BeginLSN, 32}, {b.EndLSN, 32}, {b.Compression, 64}, {b.Encryption, 64}} {
		if invalidOptionalText(field.value, field.maximum) {
			return errors.New("Barman backup contains an invalid optional value")
		}
	}
	if b.SystemID != nil && !decimal(*b.SystemID) || b.Timeline != nil && *b.Timeline == 0 || b.WALSegmentSizeBytes != nil && !validWALSegmentSize(*b.WALSegmentSizeBytes) || !validOptionalWALName(b.BeginWAL) || !validOptionalWALName(b.EndWAL) || !validOptionalLSN(b.BeginLSN) || !validOptionalLSN(b.EndLSN) || !validOptionalTime(b.BeginAt) || !validOptionalTime(b.EndAt) || b.BeginAt != nil && b.EndAt != nil && b.EndAt.Before(*b.BeginAt) {
		return errors.New("Barman backup contains invalid timeline or time evidence")
	}
	return nil
}

// Validate checks one compact WAL range and its allowlisted values.
func (r BarmanWALRange) Validate() error {
	if !validBarmanServer(r.Server) || r.Timeline == 0 || r.EndPosition < r.StartPosition || r.EndPosition == ^uint64(0) || r.SegmentCount != r.EndPosition-r.StartPosition+1 || !validWALName(r.FirstWAL) || !validWALName(r.LastWAL) || !validOptionalTime(r.LatestReceiptAt) || !validOptionalTime(r.EndReceiptAt) {
		return errors.New("Barman WAL range is invalid")
	}
	return nil
}

// Validate checks one WAL gap and its complete-generation lifecycle.
func (g BarmanWALGap) Validate() error {
	if !validBarmanServer(g.Server) || g.Timeline == 0 || g.EndPosition < g.StartPosition || g.EndPosition == ^uint64(0) || g.SegmentCount != g.EndPosition-g.StartPosition+1 || !validWALName(g.FirstWAL) || !validWALName(g.LastWAL) || (g.Status != GapCandidate && g.Status != GapConfirmed) || g.FirstObservedGeneration == 0 || g.LastObservedGeneration < g.FirstObservedGeneration || g.Status == GapCandidate && g.LastObservedGeneration != g.FirstObservedGeneration || g.Status == GapConfirmed && g.LastObservedGeneration == g.FirstObservedGeneration {
		return errors.New("Barman WAL gap is invalid")
	}
	return nil
}

// Validate checks one conservative observed recovery path.
func (p BarmanRecoveryPath) Validate() error {
	if !validBarmanServer(p.Server) || invalidText(p.BackupID, 256) || p.TargetTimeline == 0 || !validState(p.State) || invalidReason(p.Reason) || !slices.Contains([]CoverageStop{CoverageFrontier, CoverageCandidateLimited, CoverageGapLimited, CoverageUnknownLimited}, p.Stop) || !validOptionalTime(p.LowerBoundAt) || !validOptionalTime(p.FrontierReceiptAt) {
		return errors.New("Barman recovery path is invalid")
	}
	if p.AssumptionCodes == nil || len(p.AssumptionCodes) > 32 {
		return errors.New("Barman recovery assumptions are invalid")
	}
	if !completePosition(p.StartTimeline, p.StartPosition, p.StartWAL) || !completePosition(p.FrontierTimeline, p.FrontierPosition, p.FrontierWAL) {
		return errors.New("Barman recovery path positions are incomplete")
	}
	for index, code := range p.AssumptionCodes {
		if invalidReason(Reason{Code: code, Message: "bounded"}) || index > 0 && p.AssumptionCodes[index-1] >= code {
			return errors.New("Barman recovery assumptions must be bounded, unique, and sorted")
		}
	}
	return nil
}

func compareBackup(left, right BarmanBackup) int {
	if result := strings.Compare(left.BackupID, right.BackupID); result != 0 {
		return result
	}
	return strings.Compare(left.Server, right.Server)
}

func compareRange(left, right BarmanWALRange) int {
	if result := strings.Compare(left.Server, right.Server); result != 0 {
		return result
	}
	if left.Timeline != right.Timeline {
		if left.Timeline < right.Timeline {
			return -1
		}
		return 1
	}
	return compareUint64(left.StartPosition, right.StartPosition)
}

func compareGap(left, right BarmanWALGap) int {
	if result := strings.Compare(left.Server, right.Server); result != 0 {
		return result
	}
	if left.Timeline != right.Timeline {
		return compareUint64(left.Timeline, right.Timeline)
	}
	return compareUint64(left.StartPosition, right.StartPosition)
}

func compareRecoveryPath(left, right BarmanRecoveryPath) int {
	if result := strings.Compare(left.BackupID, right.BackupID); result != 0 {
		return result
	}
	if left.TargetTimeline != right.TargetTimeline {
		return compareUint64(left.TargetTimeline, right.TargetTimeline)
	}
	return strings.Compare(left.Server, right.Server)
}

func compareUint64(left, right uint64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func validState(value State) bool {
	return value == Healthy || value == Warning || value == Unhealthy || value == Unknown
}

func validSupport(value Support) bool {
	return value == Supported || value == Unsupported || value == SupportUnknown
}

func validCompleteness(value Completeness) bool {
	return value == Complete || value == Incomplete || value == Unscanned
}

func validCapability(value CapabilityID) bool {
	return slices.Contains(capabilityIDs[:], value)
}

func validFailure(value FailureCategory) bool {
	return slices.Contains([]FailureCategory{FailureCanceled, FailureTimeout, FailureInvalidConfig, FailureAuthentication, FailureAuthorization, FailureThrottled, FailureUnavailable, FailureNotFound, FailureIncompatibleFormat, FailureSafetyLimit}, value)
}

func invalidReason(value Reason) bool {
	if invalidText(value.Code, 64) || invalidText(value.Message, 256) {
		return true
	}
	for index, character := range value.Code {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || (character == '-' && index > 0 && index < len(value.Code)-1) {
			continue
		}
		return true
	}
	return false
}

func invalidText(value string, maximum int) bool {
	return value == "" || len(value) > maximum || strings.ToValidUTF8(value, "") != value || strings.IndexFunc(value, unicode.IsControl) >= 0
}

func invalidOptionalText(value *string, maximum int) bool {
	return value != nil && invalidText(*value, maximum)
}

func validOptionalTime(value *time.Time) bool {
	if value == nil {
		return true
	}
	_, offset := value.Zone()
	return !value.IsZero() && offset == 0
}

func orderedTimes(start, complete *time.Time) bool {
	return validOptionalTime(start) && validOptionalTime(complete) && start != nil && complete != nil && !complete.Before(*start)
}

func validFingerprint(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func validOptionalWALName(value *string) bool { return value == nil || validWALName(*value) }

func validWALName(value string) bool {
	return len(value) == 24 && uppercaseHex(value)
}

func validOptionalLSN(value *string) bool {
	if value == nil {
		return true
	}
	high, low, ok := strings.Cut(*value, "/")
	return ok && len(high) >= 1 && len(high) <= 8 && len(low) >= 1 && len(low) <= 8 && uppercaseHex(high) && uppercaseHex(low)
}

func uppercaseHex(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'A' || character > 'F' {
				return false
			}
		}
	}
	return true
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validWALSegmentSize(value uint64) bool {
	return value >= 1<<20 && value <= 1<<30 && value&(value-1) == 0
}

func completePosition(timeline, position *uint64, wal *string) bool {
	allNil := timeline == nil && position == nil && wal == nil
	return allNil || timeline != nil && *timeline > 0 && position != nil && wal != nil && validWALName(*wal)
}

func validBarmanServer(value string) bool {
	return !invalidText(value, 256) && value != "." && value != ".." && !strings.ContainsAny(value, "/\\")
}
