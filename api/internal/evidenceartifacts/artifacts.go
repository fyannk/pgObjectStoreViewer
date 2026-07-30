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

// Package evidenceartifacts generates the committed v1alpha1 JSON Schema and
// deterministic wire examples from the dependency-light public Go model.
package evidenceartifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	evidencev1alpha1 "github.com/fyannk/objectstoreviewer/api/evidence/v1alpha1"
)

const (
	schemaDraft = "https://json-schema.org/draft/2020-12/schema"
	schemaID    = "https://github.com/fyannk/objectstoreviewer/api/evidence/v1alpha1/schema.json"
)

var (
	timeType = reflect.TypeFor[time.Time]()
	apiPath  = reflect.TypeFor[evidencev1alpha1.RepositoryEvidenceSnapshot]().PkgPath()
)

// Artifact is one generated path relative to api/evidence/v1alpha1.
type Artifact struct {
	Path string
	Data []byte
}

// Generate returns every committed schema and wire-golden artifact.
func Generate() ([]Artifact, error) {
	schema, err := generateSchema()
	if err != nil {
		return nil, err
	}
	goldens, err := generateGoldens()
	if err != nil {
		return nil, err
	}
	return append([]Artifact{{Path: "schema.json", Data: schema}}, goldens...), nil
}

type jsonSchema map[string]any

type schemaGenerator struct {
	definitions map[string]any
	building    map[string]bool
}

func generateSchema() ([]byte, error) {
	generator := &schemaGenerator{
		definitions: make(map[string]any),
		building:    make(map[string]bool),
	}
	roots := []reflect.Type{
		reflect.TypeFor[evidencev1alpha1.RepositoryEvidenceSnapshot](),
		reflect.TypeFor[evidencev1alpha1.BarmanBackupPage](),
		reflect.TypeFor[evidencev1alpha1.BarmanWALRangePage](),
		reflect.TypeFor[evidencev1alpha1.BarmanWALGapPage](),
		reflect.TypeFor[evidencev1alpha1.BarmanRecoveryPathPage](),
		reflect.TypeFor[evidencev1alpha1.EvidenceAPIError](),
		reflect.TypeFor[evidencev1alpha1.ServiceStatus](),
	}
	rootReferences := make([]any, 0, len(roots))
	for _, root := range roots {
		generator.ensureDefinition(root)
		rootReferences = append(rootReferences, reference(root.Name()))
	}
	document := jsonSchema{
		"$schema":  schemaDraft,
		"$id":      schemaID,
		"$comment": "Generated from github.com/fyannk/objectstoreviewer/api/evidence/v1alpha1. Go Validate methods enforce cross-field, ordering, UTC, and UTF-8 byte-length invariants that JSON Schema cannot express portably.",
		"title":    "ObjectStoreViewer evidence API v1alpha1",
		"oneOf":    rootReferences,
		"$defs":    generator.definitions,
	}
	return marshalArtifact(document)
}

func (g *schemaGenerator) ensureDefinition(value reflect.Type) {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.PkgPath() != apiPath || value.Name() == "" {
		return
	}
	name := value.Name()
	if _, exists := g.definitions[name]; exists || g.building[name] {
		return
	}
	g.building[name] = true
	var definition jsonSchema
	switch name {
	case "Details":
		definition = g.detailsDefinition()
	case "ServiceStatus":
		definition = serviceStatusDefinition()
	default:
		definition = g.definition(value)
	}
	g.definitions[name] = definition
	delete(g.building, name)
}

func (g *schemaGenerator) definition(value reflect.Type) jsonSchema {
	if enum := enumValues(value); enum != nil {
		return jsonSchema{"type": "string", "enum": enum}
	}
	switch value.Kind() {
	case reflect.Struct:
		return g.structDefinition(value)
	case reflect.String:
		return jsonSchema{"type": "string"}
	default:
		panic("unsupported public schema type " + value.String())
	}
}

func (g *schemaGenerator) structDefinition(value reflect.Type) jsonSchema {
	properties := make(map[string]any)
	required := make([]string, 0, value.NumField())
	g.collectFields(value, value, properties, &required)
	return objectDefinition(properties, required)
}

func (g *schemaGenerator) collectFields(owner, value reflect.Type, properties map[string]any, required *[]string) {
	for index := range value.NumField() {
		field := value.Field(index)
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "-" || field.PkgPath != "" {
			continue
		}
		if field.Anonymous && jsonName == "" {
			g.collectFields(owner, field.Type, properties, required)
			continue
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		property := g.schemaFor(field.Type)
		applyFieldConstraints(owner.Name(), field.Name, property)
		properties[jsonName] = property
		*required = append(*required, jsonName)
	}
}

func (g *schemaGenerator) schemaFor(value reflect.Type) jsonSchema {
	if value == timeType {
		return jsonSchema{"type": "string", "format": "date-time"}
	}
	if value.Kind() == reflect.Pointer {
		return jsonSchema{"anyOf": []any{g.schemaFor(value.Elem()), jsonSchema{"type": "null"}}}
	}
	if value.Name() != "" && value.PkgPath() == apiPath {
		g.ensureDefinition(value)
		return reference(value.Name())
	}
	switch value.Kind() {
	case reflect.String:
		return jsonSchema{"type": "string"}
	case reflect.Bool:
		return jsonSchema{"type": "boolean"}
	case reflect.Uint64:
		return jsonSchema{"type": "integer", "minimum": uint64(0), "maximum": ^uint64(0)}
	case reflect.Slice:
		return jsonSchema{"type": "array", "items": g.schemaFor(value.Elem())}
	default:
		panic("unsupported public schema field " + value.String())
	}
}

func (g *schemaGenerator) detailsDefinition() jsonSchema {
	g.ensureDefinition(reflect.TypeFor[evidencev1alpha1.BarmanCloudSummary]())
	known := objectDefinition(map[string]any{
		"type":         jsonSchema{"const": evidencev1alpha1.BarmanDetailsType},
		"barman_cloud": reference("BarmanCloudSummary"),
	}, []string{"type", "barman_cloud"})
	unknown := objectDefinition(map[string]any{
		"type": boundedText(64),
	}, []string{"type"})
	unknown["not"] = jsonSchema{
		"properties": jsonSchema{"type": jsonSchema{"const": evidencev1alpha1.BarmanDetailsType}},
		"required":   []string{"type"},
	}
	return jsonSchema{"oneOf": []any{known, unknown}}
}

func serviceStatusDefinition() jsonSchema {
	branches := make([]any, 0, 3)
	for _, pair := range [][2]string{
		{evidencev1alpha1.HealthKind, evidencev1alpha1.HealthLive},
		{evidencev1alpha1.ReadinessKind, evidencev1alpha1.ReadinessReady},
		{evidencev1alpha1.ReadinessKind, evidencev1alpha1.ReadinessNotReady},
	} {
		branches = append(branches, objectDefinition(map[string]any{
			"api_version": jsonSchema{"const": evidencev1alpha1.APIVersion},
			"kind":        jsonSchema{"const": pair[0]},
			"status":      jsonSchema{"const": pair[1]},
		}, []string{"api_version", "kind", "status"}))
	}
	return jsonSchema{"oneOf": branches}
}

func objectDefinition(properties map[string]any, required []string) jsonSchema {
	return jsonSchema{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": true,
	}
}

func reference(name string) jsonSchema {
	return jsonSchema{"$ref": "#/$defs/" + name}
}

func enumValues(value reflect.Type) []any {
	var values []string
	switch value.Name() {
	case "State":
		values = []string{string(evidencev1alpha1.Healthy), string(evidencev1alpha1.Warning), string(evidencev1alpha1.Unhealthy), string(evidencev1alpha1.Unknown)}
	case "Support":
		values = []string{string(evidencev1alpha1.Supported), string(evidencev1alpha1.Unsupported), string(evidencev1alpha1.SupportUnknown)}
	case "Completeness":
		values = []string{string(evidencev1alpha1.Complete), string(evidencev1alpha1.Incomplete), string(evidencev1alpha1.Unscanned)}
	case "CapabilityID":
		values = []string{
			string(evidencev1alpha1.ObjectInventory), string(evidencev1alpha1.CatalogListing), string(evidencev1alpha1.StructuralValidation),
			string(evidencev1alpha1.DependencyValidation), string(evidencev1alpha1.WALContinuity), string(evidencev1alpha1.TimelineTraversal),
			string(evidencev1alpha1.EncryptedMetadata), string(evidencev1alpha1.RecoveryCoverage), string(evidencev1alpha1.RetentionExpectation),
		}
	case "FailureCategory":
		values = []string{
			string(evidencev1alpha1.FailureCanceled), string(evidencev1alpha1.FailureTimeout), string(evidencev1alpha1.FailureInvalidConfig),
			string(evidencev1alpha1.FailureAuthentication), string(evidencev1alpha1.FailureAuthorization), string(evidencev1alpha1.FailureThrottled),
			string(evidencev1alpha1.FailureUnavailable), string(evidencev1alpha1.FailureNotFound), string(evidencev1alpha1.FailureIncompatibleFormat),
			string(evidencev1alpha1.FailureSafetyLimit),
		}
	case "GapStatus":
		values = []string{string(evidencev1alpha1.GapCandidate), string(evidencev1alpha1.GapConfirmed)}
	case "CoverageStop":
		values = []string{
			string(evidencev1alpha1.CoverageFrontier), string(evidencev1alpha1.CoverageCandidateLimited),
			string(evidencev1alpha1.CoverageGapLimited), string(evidencev1alpha1.CoverageUnknownLimited),
		}
	case "ErrorCode":
		values = []string{
			string(evidencev1alpha1.ErrorInvalidRequest), string(evidencev1alpha1.ErrorUnauthenticated), string(evidencev1alpha1.ErrorNotFound),
			string(evidencev1alpha1.ErrorMethodNotAllowed), string(evidencev1alpha1.ErrorPublicationChanged), string(evidencev1alpha1.ErrorResponseLimit),
			string(evidencev1alpha1.ErrorBusy), string(evidencev1alpha1.ErrorInvalidPublication),
		}
	default:
		return nil
	}
	result := make([]any, len(values))
	for index, item := range values {
		result[index] = item
	}
	return result
}

func applyFieldConstraints(owner, field string, schema jsonSchema) {
	key := owner + "." + field
	switch key {
	case "RepositoryEvidenceSnapshot.APIVersion", "BarmanBackupPage.APIVersion", "BarmanWALRangePage.APIVersion", "BarmanWALGapPage.APIVersion", "BarmanRecoveryPathPage.APIVersion", "EvidenceAPIError.APIVersion":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.APIVersion})
	case "RepositoryEvidenceSnapshot.Kind":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.SnapshotKind})
	case "BarmanBackupPage.Kind":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.BarmanBackupPageKind})
	case "BarmanWALRangePage.Kind":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.BarmanWALRangePageKind})
	case "BarmanWALGapPage.Kind":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.BarmanWALGapPageKind})
	case "BarmanRecoveryPathPage.Kind":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.BarmanRecoveryPageKind})
	case "EvidenceAPIError.Kind":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.ErrorKind})
	case "Producer.Name":
		replaceSchema(schema, jsonSchema{"const": evidencev1alpha1.ProducerName})
	case "RepositoryIdentity.Provider":
		replaceSchema(schema, jsonSchema{"const": "s3"})
	case "RepositoryIdentity.Format":
		replaceSchema(schema, jsonSchema{"const": "barman-cloud"})
	case "ScopeIdentity.Kind":
		replaceSchema(schema, jsonSchema{"const": "barman-server"})
	case "Reason.Code":
		setText(schema, 64, `^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	case "Reason.Message", "EvidenceAPIError.Message":
		setText(schema, 256, "")
	case "Producer.Version":
		setText(schema, 64, "")
	case "ClusterIdentity.Namespace":
		setText(schema, 63, "")
	case "ClusterIdentity.UID":
		setText(schema, 128, "")
	case "ClusterIdentity.Name":
		setText(schema, 253, "")
	case "RepositoryIdentity.DestinationFingerprint":
		setText(schema, 71, `^sha256:[0-9a-f]{64}$`)
	case "ScopeIdentity.Name", "BarmanBackup.Server", "BarmanWALRange.Server", "BarmanWALGap.Server", "BarmanRecoveryPath.Server":
		setText(schema, 256, `^[^/\\]+$`)
	case "BarmanBackup.BackupID", "BarmanRecoveryPath.BackupID":
		setText(schema, 256, "")
	case "BarmanBackup.Status", "BarmanBackup.BackupType", "BarmanBackup.Compression", "BarmanBackup.Encryption":
		setText(schema, 64, "")
	case "BarmanBackup.SystemID":
		setText(schema, 64, `^[0-9]+$`)
	case "BarmanBackup.BeginWAL", "BarmanBackup.EndWAL", "BarmanWALRange.FirstWAL", "BarmanWALRange.LastWAL", "BarmanWALGap.FirstWAL", "BarmanWALGap.LastWAL", "BarmanRecoveryPath.StartWAL", "BarmanRecoveryPath.FrontierWAL":
		setText(schema, 24, `^[0-9A-F]{24}$`)
	case "BarmanBackup.BeginLSN", "BarmanBackup.EndLSN":
		setText(schema, 17, `^[0-9A-F]{1,8}/[0-9A-F]{1,8}$`)
	case "BarmanBackup.WALSegmentSizeBytes":
		setEnum(schema, walSegmentSizes())
	case "BarmanBackup.Timeline", "BarmanWALRange.Timeline", "BarmanWALGap.Timeline", "BarmanRecoveryPath.TargetTimeline", "BarmanRecoveryPath.StartTimeline", "BarmanRecoveryPath.FrontierTimeline", "BarmanWALRange.SegmentCount", "BarmanWALGap.SegmentCount", "BarmanWALGap.FirstObservedGeneration":
		setMinimum(schema, 1)
	case "BarmanBackupPage.Revision", "BarmanBackupPage.EvidenceGeneration", "BarmanWALRangePage.Revision", "BarmanWALRangePage.EvidenceGeneration", "BarmanWALGapPage.Revision", "BarmanWALGapPage.EvidenceGeneration", "BarmanRecoveryPathPage.Revision", "BarmanRecoveryPathPage.EvidenceGeneration":
		setMinimum(schema, 1)
	case "BarmanBackupPage.NextCursor", "BarmanWALRangePage.NextCursor", "BarmanWALGapPage.NextCursor", "BarmanRecoveryPathPage.NextCursor":
		setText(schema, 4096, "")
	case "RepositoryEvidenceSnapshot.Capabilities":
		schema["minItems"], schema["maxItems"] = 9, 9
	case "BarmanBackupPage.Items", "BarmanWALRangePage.Items", "BarmanWALGapPage.Items", "BarmanRecoveryPathPage.Items":
		schema["maxItems"] = 200
	case "BarmanRecoveryPath.AssumptionCodes":
		schema["maxItems"], schema["uniqueItems"] = 32, true
		if items, ok := schema["items"].(jsonSchema); ok {
			setText(items, 64, `^[a-z0-9]+(?:-[a-z0-9]+)*$`)
		}
	}
}

func replaceSchema(target, replacement jsonSchema) {
	clear(target)
	for key, value := range replacement {
		target[key] = value
	}
}

func setText(schema jsonSchema, maximum int, pattern string) {
	applyNonNull(schema, func(value jsonSchema) {
		value["minLength"] = 1
		value["maxLength"] = maximum
		value["x-max-utf8-bytes"] = maximum
		if pattern != "" {
			value["pattern"] = pattern
		}
	})
}

func boundedText(maximum int) jsonSchema {
	result := jsonSchema{"type": "string"}
	setText(result, maximum, "")
	return result
}

func setMinimum(schema jsonSchema, minimum uint64) {
	applyNonNull(schema, func(value jsonSchema) { value["minimum"] = minimum })
}

func setEnum(schema jsonSchema, values []any) {
	applyNonNull(schema, func(value jsonSchema) { value["enum"] = values })
}

func applyNonNull(schema jsonSchema, apply func(jsonSchema)) {
	if alternatives, ok := schema["anyOf"].([]any); ok {
		for _, alternative := range alternatives {
			candidate, ok := alternative.(jsonSchema)
			if ok && candidate["type"] != "null" {
				apply(candidate)
				return
			}
		}
	}
	apply(schema)
}

func walSegmentSizes() []any {
	result := make([]any, 0, 11)
	for value := uint64(1 << 20); value <= 1<<30; value <<= 1 {
		result = append(result, value)
	}
	return result
}

func generateGoldens() ([]Artifact, error) {
	started := time.Date(2026, 7, 28, 10, 0, 0, 123000000, time.UTC)
	completed := started.Add(3 * time.Second)
	archiveReceipt := completed.Add(2 * time.Minute)
	fingerprint, err := evidencev1alpha1.FingerprintS3(evidencev1alpha1.S3FingerprintInput{
		Region: "eu-west-1", Bucket: "synthetic-bucket", Prefix: "cluster",
		Format: "barman-cloud", ScopeKind: "barman-server", ScopeName: "orders",
	})
	if err != nil {
		return nil, err
	}
	reason := evidencev1alpha1.Reason{Code: "evidence-evaluated", Message: "structural evidence evaluated"}
	result := evidencev1alpha1.StateReason{State: evidencev1alpha1.Healthy, Reason: reason}
	backupCount, rangeCount, gapCount, pathCount := uint64(1), uint64(1), uint64(1), uint64(1)
	zero, one, two := uint64(0), uint64(1), uint64(2)
	clusterName := "orders"
	snapshot := evidencev1alpha1.RepositoryEvidenceSnapshot{
		APIVersion: evidencev1alpha1.APIVersion,
		Kind:       evidencev1alpha1.SnapshotKind,
		Producer:   evidencev1alpha1.Producer{Name: evidencev1alpha1.ProducerName, Version: "1.0.0"},
		Identity: evidencev1alpha1.Identity{
			Cluster: evidencev1alpha1.ClusterIdentity{Namespace: "database-team", UID: "2f12b7d1-7e8d-4c37-a68f-233efc5f3191", Name: &clusterName},
			Repository: evidencev1alpha1.RepositoryIdentity{
				Provider: "s3", Format: "barman-cloud", DestinationFingerprint: fingerprint,
				Scope: evidencev1alpha1.ScopeIdentity{Kind: "barman-server", Name: "orders"},
			},
		},
		Revision: 42, EvidenceGeneration: 42, StartedAt: &started, CompletedAt: &completed, LastAttemptAt: &started,
		Completeness: evidencev1alpha1.Complete, State: evidencev1alpha1.Healthy, Reason: reason,
		Capabilities: goldenCapabilities(reason),
		Inventory: evidencev1alpha1.InventorySummary{
			Known: true, ObjectCount: &backupCount, StoredBytes: ptr(uint64(4096)), UnscopedObjectCount: &zero,
			PagesExamined: one, ObjectsExamined: backupCount,
		},
		Details: evidencev1alpha1.Details{Type: evidencev1alpha1.BarmanDetailsType, BarmanCloud: &evidencev1alpha1.BarmanCloudSummary{
			BackupItems: &backupCount, WALRangeItems: &rangeCount, WALGapItems: &gapCount, RecoveryPathItems: &pathCount,
			StructurallyUsableBackups: &backupCount,
			BackupStates:              &evidencev1alpha1.StateCounts{Healthy: one},
			WALCounts:                 &evidencev1alpha1.BarmanWALCounts{Segment: two},
			WAL:                       result,
			Timeline:                  result,
			Coverage:                  result,
			Retention: evidencev1alpha1.BarmanRetentionSummary{
				VisibleBackups: &backupCount, StructurallyUsableBackups: &backupCount,
				OldestCompletionAt: &completed, NewestCompletionAt: &completed, State: evidencev1alpha1.Healthy, Reason: reason,
			},
			LatestArchiveReceiptAt: &archiveReceipt,
		}},
	}
	header := func(kind string) evidencev1alpha1.PageHeader {
		return evidencev1alpha1.PageHeader{
			APIVersion: evidencev1alpha1.APIVersion, Kind: kind, Revision: 42, EvidenceGeneration: 42, TotalItems: &one,
		}
	}
	status, backupType, systemID := "DONE", "full", "7396242311909787798"
	walSize, timeline := uint64(16<<20), uint64(1)
	beginWAL, endWAL := "000000010000000000000001", "000000010000000000000002"
	beginLSN, endLSN := "0/1000000", "0/2000000"
	compression, encryption := "gzip", "none"
	logical, stored := uint64(8192), uint64(4096)
	backupPage := evidencev1alpha1.BarmanBackupPage{
		PageHeader: header(evidencev1alpha1.BarmanBackupPageKind),
		Items: []evidencev1alpha1.BarmanBackup{{
			Server: "orders", BackupID: "20260728T100000", Status: &status, BackupType: &backupType,
			State: evidencev1alpha1.Healthy, Reason: reason, SystemID: &systemID, PostgreSQLVersion: ptr(uint64(170000)),
			Timeline: &timeline, WALSegmentSizeBytes: &walSize, BeginWAL: &beginWAL, EndWAL: &endWAL,
			BeginLSN: &beginLSN, EndLSN: &endLSN, BeginAt: &started, EndAt: &completed,
			LogicalBytes: &logical, StoredArtifactBytes: &stored, Compression: &compression, Encryption: &encryption,
			ArtifactCount: &one, TablespaceCount: &zero,
		}},
	}
	rangePage := evidencev1alpha1.BarmanWALRangePage{
		PageHeader: header(evidencev1alpha1.BarmanWALRangePageKind),
		Items: []evidencev1alpha1.BarmanWALRange{{
			Server: "orders", Timeline: one, StartPosition: one, EndPosition: two, SegmentCount: two,
			FirstWAL: beginWAL, LastWAL: endWAL, LatestReceiptAt: &archiveReceipt, EndReceiptAt: &archiveReceipt,
		}},
	}
	gapPage := evidencev1alpha1.BarmanWALGapPage{
		PageHeader: header(evidencev1alpha1.BarmanWALGapPageKind),
		Items: []evidencev1alpha1.BarmanWALGap{{
			Server: "orders", Timeline: one, StartPosition: 3, EndPosition: 3, SegmentCount: one,
			FirstWAL: "000000010000000000000003", LastWAL: "000000010000000000000003",
			Status: evidencev1alpha1.GapCandidate, FirstObservedGeneration: 42, LastObservedGeneration: 42,
		}},
	}
	recoveryPage := evidencev1alpha1.BarmanRecoveryPathPage{
		PageHeader: header(evidencev1alpha1.BarmanRecoveryPageKind),
		Items: []evidencev1alpha1.BarmanRecoveryPath{{
			Server: "orders", BackupID: "20260728T100000", TargetTimeline: one,
			State: evidencev1alpha1.Healthy, Reason: reason, Stop: evidencev1alpha1.CoverageFrontier,
			LowerBoundAt: &completed, StartTimeline: &one, StartPosition: &one, StartWAL: &beginWAL,
			FrontierTimeline: &one, FrontierPosition: &two, FrontierWAL: &endWAL, FrontierReceiptAt: &archiveReceipt,
			AssumptionCodes: []string{"segment-name-presence-only", "wal-bytes-and-restore-not-verified"},
		}},
	}
	values := []struct {
		path  string
		value any
	}{
		{"testdata/wire/health.json", evidencev1alpha1.ServiceStatus{APIVersion: evidencev1alpha1.APIVersion, Kind: evidencev1alpha1.HealthKind, Status: evidencev1alpha1.HealthLive}},
		{"testdata/wire/readiness-ready.json", evidencev1alpha1.ServiceStatus{APIVersion: evidencev1alpha1.APIVersion, Kind: evidencev1alpha1.ReadinessKind, Status: evidencev1alpha1.ReadinessReady}},
		{"testdata/wire/readiness-not-ready.json", evidencev1alpha1.ServiceStatus{APIVersion: evidencev1alpha1.APIVersion, Kind: evidencev1alpha1.ReadinessKind, Status: evidencev1alpha1.ReadinessNotReady}},
		{"testdata/wire/snapshot.json", snapshot},
		{"testdata/wire/backups.json", backupPage},
		{"testdata/wire/wal-ranges.json", rangePage},
		{"testdata/wire/wal-gaps.json", gapPage},
		{"testdata/wire/recovery-paths.json", recoveryPage},
		{"testdata/wire/error.json", evidencev1alpha1.EvidenceAPIError{APIVersion: evidencev1alpha1.APIVersion, Kind: evidencev1alpha1.ErrorKind, Code: evidencev1alpha1.ErrorPublicationChanged, Message: "repository evidence changed; restart from snapshot"}},
	}
	artifacts := make([]Artifact, 0, len(values))
	for _, value := range values {
		if err := validateGolden(value.value); err != nil {
			return nil, fmt.Errorf("generate %s: %w", value.path, err)
		}
		data, err := marshalArtifact(value.value)
		if err != nil {
			return nil, fmt.Errorf("generate %s: %w", value.path, err)
		}
		artifacts = append(artifacts, Artifact{Path: value.path, Data: data})
	}
	return artifacts, nil
}

func goldenCapabilities(reason evidencev1alpha1.Reason) []evidencev1alpha1.Capability {
	ids := []evidencev1alpha1.CapabilityID{
		evidencev1alpha1.CatalogListing,
		evidencev1alpha1.DependencyValidation,
		evidencev1alpha1.EncryptedMetadata,
		evidencev1alpha1.ObjectInventory,
		evidencev1alpha1.RecoveryCoverage,
		evidencev1alpha1.RetentionExpectation,
		evidencev1alpha1.StructuralValidation,
		evidencev1alpha1.TimelineTraversal,
		evidencev1alpha1.WALContinuity,
	}
	result := make([]evidencev1alpha1.Capability, 0, len(ids))
	for _, id := range ids {
		result = append(result, evidencev1alpha1.Capability{ID: id, Support: evidencev1alpha1.Supported, State: evidencev1alpha1.Healthy, Reason: reason})
	}
	return result
}

func validateGolden(value any) error {
	var err error
	switch typed := value.(type) {
	case evidencev1alpha1.RepositoryEvidenceSnapshot:
		err = typed.Validate()
	case evidencev1alpha1.BarmanBackupPage:
		err = typed.Validate()
	case evidencev1alpha1.BarmanWALRangePage:
		err = typed.Validate()
	case evidencev1alpha1.BarmanWALGapPage:
		err = typed.Validate()
	case evidencev1alpha1.BarmanRecoveryPathPage:
		err = typed.Validate()
	case evidencev1alpha1.EvidenceAPIError:
		err = typed.Validate()
	case evidencev1alpha1.ServiceStatus:
		err = typed.Validate()
	default:
		err = errors.New("unsupported golden resource")
	}
	return err
}

func marshalArtifact(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func ptr[T any](value T) *T { return &value }
