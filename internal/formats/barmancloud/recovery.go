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

package barmancloud

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/ulikunitz/xz"

	"github.com/fyannk/pgObjectStoreViewer/internal/evidence"
	"github.com/fyannk/pgObjectStoreViewer/internal/store"
)

const (
	MaxHistoryObjectBytes       int64 = 256 * 1024
	MaxHistoryDecompressedBytes int64 = 256 * 1024
	MaxHistoryEntries                 = 128
	MaxRecoveryPaths                  = 10_000
)

type CoverageStop string

const (
	CoverageFrontier         CoverageStop = "frontier"
	CoverageCandidateLimited CoverageStop = "candidate-limited"
	CoverageGapLimited       CoverageStop = "gap-limited"
	CoverageUnknownLimited   CoverageStop = "unknown-limited"
)

type HistoryEdge struct {
	Parent, Child  uint32
	SwitchLSN      uint64
	SwitchPosition uint64
	SwitchWAL      string
}

type TimelineHistory struct {
	Server, Key string
	Timeline    uint32
	State       evidence.State
	Reason      string
	Edges       []HistoryEdge
}

type RecoveryPath struct {
	Server, BackupID                string
	TargetTimeline                  uint32
	State                           evidence.State
	Reason                          string
	Stop                            CoverageStop
	LowerBound                      time.Time
	StartTimeline, FrontierTimeline uint32
	StartPosition, FrontierPosition uint64
	StartWAL, FrontierWAL           string
	FrontierReceipt                 time.Time
	Assumptions                     []string
}

type RetentionSummary struct {
	VisibleBackups, StructurallyUsable int
	OldestCompletion, NewestCompletion time.Time
	MinimumConfigured                  bool
	MinimumRedundancy                  int
	PolicyConfigured                   bool
	State                              evidence.State
	Reason                             string
}

type ServerRecovery struct {
	Server         string
	TimelineState  evidence.State
	TimelineReason string
	CoverageState  evidence.State
	CoverageReason string
	Histories      []TimelineHistory
	Paths          []RecoveryPath
	Retention      RetentionSummary
}

type RecoveryCatalog struct {
	Servers []ServerRecovery
}

type RecoveryOptions struct {
	ExpectedRetentionPolicy   string
	ExpectedMinimumRedundancy *int
}

// ParseHistory parses PostgreSQL's tab-separated ancestor/switchpoint format.
// Human-readable reason text is validated for boundedness and discarded.
func ParseHistory(data []byte, target uint32, segmentSize int64) ([]HistoryEdge, error) {
	if len(data) == 0 || int64(len(data)) > MaxHistoryDecompressedBytes || target <= 1 || !ValidWALSegmentSize(segmentSize) {
		return nil, errors.New("invalid bounded timeline history")
	}
	var entries []struct {
		parent uint32
		lsn    uint64
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 4096)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || strings.TrimSpace(fields[2]) == "" || len(fields[2]) > 1024 || strings.IndexFunc(fields[2], unicode.IsControl) >= 0 {
			return nil, errors.New("malformed timeline history record")
		}
		parentValue, err := strconv.ParseUint(strings.TrimSpace(fields[0]), 10, 32)
		if err != nil || parentValue == 0 {
			return nil, errors.New("malformed timeline parent")
		}
		lsn, err := ParseLSN(strings.TrimSpace(fields[1]))
		if err != nil || lsn == 0 {
			return nil, errors.New("malformed timeline switchpoint")
		}
		entries = append(entries, struct {
			parent uint32
			lsn    uint64
		}{uint32(parentValue), lsn})
		if len(entries) > MaxHistoryEntries {
			return nil, errors.New("timeline history entry limit reached")
		}
	}
	if err := scanner.Err(); err != nil || len(entries) == 0 {
		return nil, errors.New("malformed timeline history")
	}
	edges := make([]HistoryEdge, len(entries))
	for index, entry := range entries {
		if entry.parent >= target || (index > 0 && (entry.parent <= entries[index-1].parent || entry.lsn <= entries[index-1].lsn)) {
			return nil, errors.New("impossible timeline ancestry")
		}
		child := target
		if index+1 < len(entries) {
			child = entries[index+1].parent
		}
		// #nosec G115 -- ParseHistory rejects any segmentSize failing
		// ValidWALSegmentSize, so it is a power of two from 1 MiB to 1 GiB.
		position := entry.lsn / uint64(segmentSize)
		name, err := WALName(child, position, segmentSize)
		if err != nil {
			return nil, errors.New("timeline switchpoint exceeds WAL range")
		}
		edges[index] = HistoryEdge{Parent: entry.parent, Child: child, SwitchLSN: entry.lsn, SwitchPosition: position, SwitchWAL: name}
	}
	return edges, nil
}

func ParseLSN(value string) (uint64, error) {
	high, low, ok := strings.Cut(value, "/")
	if !ok || high == "" || low == "" || len(high) > 8 || len(low) > 8 || !isHex(high) || !isHex(low) {
		return 0, errors.New("invalid LSN")
	}
	hi, errHigh := strconv.ParseUint(high, 16, 32)
	lo, errLow := strconv.ParseUint(low, 16, 32)
	if errHigh != nil || errLow != nil {
		return 0, errors.New("invalid LSN")
	}
	return hi<<32 | lo, nil
}

func AnalyzeRecovery(ctx context.Context, reader store.Reader, backups []Backup, wal WALCatalog, historyObjects []HistoryObject, historyTruncated bool, options RecoveryOptions) RecoveryCatalog {
	serverNames := make(map[string]struct{})
	for _, server := range wal.Servers {
		serverNames[server.Server] = struct{}{}
	}
	for _, backup := range backups {
		serverNames[backup.Server] = struct{}{}
	}
	for _, object := range historyObjects {
		serverNames[object.Server] = struct{}{}
	}
	result := RecoveryCatalog{Servers: make([]ServerRecovery, 0, len(serverNames))}
	for serverName := range serverNames {
		serverWAL := previousServer(wal, serverName)
		if serverWAL.Server == "" {
			serverWAL.Server = serverName
		}
		serverBackups := backupsForServer(backups, serverName)
		serverObjects := historiesForServer(historyObjects, serverName)
		server := ServerRecovery{Server: serverName}
		server.Histories, server.TimelineState, server.TimelineReason = analyzeHistories(ctx, reader, serverWAL, serverObjects, historyTruncated)
		server.Paths = recoveryPaths(serverBackups, serverWAL, server.Histories)
		server.CoverageState, server.CoverageReason = coverageRollup(server.Paths)
		server.Retention = retentionSummary(serverBackups, options)
		result.Servers = append(result.Servers, server)
	}
	sort.Slice(result.Servers, func(i, j int) bool { return result.Servers[i].Server < result.Servers[j].Server })
	return result
}

func analyzeHistories(ctx context.Context, reader store.Reader, wal ServerWAL, objects []HistoryObject, truncated bool) ([]TimelineHistory, evidence.State, string) {
	if truncated {
		return nil, evidence.Unknown, "timeline history file limit reached"
	}
	byTimeline := make(map[uint32][]HistoryObject)
	for _, object := range objects {
		byTimeline[object.Timeline] = append(byTimeline[object.Timeline], object)
	}
	observed := observedTimelines(wal)
	for _, object := range objects {
		observed[object.Timeline] = struct{}{}
	}
	result := make([]TimelineHistory, 0, len(byTimeline))
	allEdges := make(map[uint32]HistoryEdge)
	knownChildren := make(map[uint32]bool)
	state, reason := evidence.Healthy, "all required timeline histories parsed"
	timelines := make([]uint32, 0, len(byTimeline))
	for timeline := range byTimeline {
		timelines = append(timelines, timeline)
	}
	slices.Sort(timelines)
	for _, timeline := range timelines {
		listed := byTimeline[timeline]
		history := TimelineHistory{Server: wal.Server, Timeline: timeline, State: evidence.Unknown}
		if len(listed) != 1 {
			history.Reason = "duplicate timeline history objects"
		} else {
			history.Key = listed[0].Key
			data, err := readHistory(ctx, reader, listed[0])
			if err != nil {
				history.Reason = "timeline history could not be read"
			} else if history.Edges, err = ParseHistory(data, timeline, wal.SegmentSize); err != nil {
				history.Reason = "timeline history is malformed"
			} else if !historyEdgesAgree(history.Edges, allEdges) {
				history.Reason = "timeline histories contradict one another"
			} else if cycleInEdges(allEdges) {
				history.Reason = "timeline ancestry is cyclic"
			} else if !childRangesAgree(history.Edges, wal.Ranges) {
				history.Reason = "child WAL range disagrees with timeline switchpoint"
			} else {
				history.State, history.Reason = evidence.Healthy, "timeline ancestry parsed"
				for _, edge := range history.Edges {
					knownChildren[edge.Child] = true
				}
			}
		}
		if history.State != evidence.Healthy {
			state, reason = evidence.Unknown, "timeline ancestry contains missing or invalid evidence"
		}
		result = append(result, history)
	}
	for timeline := range observed {
		if timeline > 1 && len(byTimeline[timeline]) == 0 && !knownChildren[timeline] {
			result = append(result, TimelineHistory{Server: wal.Server, Timeline: timeline, State: evidence.Unknown, Reason: "required timeline history is missing"})
			state, reason = evidence.Unknown, "timeline ancestry contains missing or invalid evidence"
		}
	}
	if len(observed) == 0 {
		state, reason = evidence.Unknown, "no WAL timelines observed"
	} else if len(result) == 0 {
		reason = "timeline 1 requires no history file"
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Timeline < result[j].Timeline })
	return result, state, reason
}

func readHistory(ctx context.Context, reader store.Reader, object HistoryObject) ([]byte, error) {
	stream, err := reader.Open(ctx, store.OpenRequest{Key: object.Key, MaxBytes: MaxHistoryObjectBytes})
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	compressed, err := io.ReadAll(io.LimitReader(stream, MaxHistoryObjectBytes+1))
	if err != nil || int64(len(compressed)) > MaxHistoryObjectBytes {
		return nil, errors.New("history object exceeds limit")
	}
	readerValue, closeValue, err := historyReader(bytes.NewReader(compressed), object.Compression)
	if err != nil {
		return nil, err
	}
	if closeValue != nil {
		defer func() { _ = closeValue.Close() }()
	}
	data, err := io.ReadAll(io.LimitReader(readerValue, MaxHistoryDecompressedBytes+1))
	if err != nil || int64(len(data)) > MaxHistoryDecompressedBytes {
		return nil, errors.New("decompressed history exceeds limit")
	}
	return data, nil
}

func historyReader(source io.Reader, compression string) (io.Reader, io.Closer, error) {
	switch compression {
	case "none":
		return source, nil, nil
	case "gzip":
		reader, err := gzip.NewReader(source)
		return reader, reader, err
	case "bzip2":
		return bzip2.NewReader(source), nil, nil
	case "snappy":
		return snappy.NewReader(source), nil, nil
	case "lz4":
		return lz4.NewReader(source), nil, nil
	case "xz":
		reader, err := xz.NewReader(source)
		return reader, nil, err
	case "zstd":
		reader, err := zstd.NewReader(source, zstd.WithDecoderMaxMemory(uint64(MaxHistoryDecompressedBytes*4)))
		if err != nil {
			return nil, nil, err
		}
		closer := reader.IOReadCloser()
		return closer, closer, nil
	default:
		return nil, nil, errors.New("unsupported history compression")
	}
}

func recoveryPaths(backups []Backup, wal ServerWAL, histories []TimelineHistory) []RecoveryPath {
	result := make([]RecoveryPath, 0)
	if conflictingSystemIDs(backups) {
		for _, backup := range backups {
			if backup.State != evidence.Healthy {
				continue
			}
			path := unknownPath(backup, "backup metadata has conflicting PostgreSQL system identifiers")
			path.TargetTimeline = backup.Timeline
			result = append(result, path)
		}
		sortRecoveryPaths(result)
		return result
	}
	for _, backup := range backups {
		if backup.State != evidence.Healthy {
			continue
		}
		startTimeline, start, end, err := backupRange(backup)
		if err != nil || wal.SegmentSize != backup.SegmentSize {
			result = append(result, unknownPath(backup, "backup WAL anchors or arithmetic context are invalid"))
			continue
		}
		own := evaluatePath(backup, wal, startTimeline, start, end, startTimeline, nil)
		result = append(result, own)
		seenTargets := map[uint32]bool{startTimeline: true}
		notApplicableTargets := make(map[uint32]bool)
		for _, history := range histories {
			if history.State != evidence.Healthy || history.Timeline == startTimeline {
				continue
			}
			edges, ok := descendantEdges(history.Edges, startTimeline)
			if !ok || len(edges) == 0 {
				continue
			}
			if branchBeforeBackup(edges[0], backup, end) {
				for _, edge := range edges {
					notApplicableTargets[edge.Child] = true
				}
				continue
			}
			for edgeIndex, edge := range edges {
				if seenTargets[edge.Child] {
					continue
				}
				seenTargets[edge.Child] = true
				result = append(result, evaluatePath(backup, wal, startTimeline, start, end, edge.Child, edges[:edgeIndex+1]))
				if len(result) >= MaxRecoveryPaths {
					result[len(result)-1] = unknownPath(backup, "recovery path safety limit reached")
					sortRecoveryPaths(result)
					return result
				}
			}
		}
		observedTargets := make([]uint32, 0)
		for timeline := range observedTimelines(wal) {
			if timeline > startTimeline && !seenTargets[timeline] && !notApplicableTargets[timeline] {
				observedTargets = append(observedTargets, timeline)
			}
		}
		slices.Sort(observedTargets)
		for _, target := range observedTargets {
			path := unknownPath(backup, "observed target timeline is not connected by valid history evidence")
			path.TargetTimeline = target
			result = append(result, path)
			if len(result) >= MaxRecoveryPaths {
				result[len(result)-1] = unknownPath(backup, "recovery path safety limit reached")
				sortRecoveryPaths(result)
				return result
			}
		}
	}
	sortRecoveryPaths(result)
	return result
}

func sortRecoveryPaths(result []RecoveryPath) {
	sort.Slice(result, func(i, j int) bool {
		if result[i].BackupID != result[j].BackupID {
			return result[i].BackupID < result[j].BackupID
		}
		return result[i].TargetTimeline < result[j].TargetTimeline
	})
}

func evaluatePath(backup Backup, wal ServerWAL, timeline uint32, start, requiredEnd uint64, target uint32, edges []HistoryEdge) RecoveryPath {
	path := RecoveryPath{Server: backup.Server, BackupID: backup.ID, TargetTimeline: target, LowerBound: backup.EndAt.UTC(), StartTimeline: timeline, StartPosition: start, StartWAL: backup.BeginWAL, Assumptions: recoveryAssumptions(backup)}
	if wal.State == evidence.Unknown {
		path.State, path.Stop, path.Reason = evidence.Unknown, CoverageUnknownLimited, "WAL continuity contains unknown evidence"
		return path
	}
	current, position := timeline, start
	frontier, receipt, state, stop, reason := reachPosition(wal, current, position, requiredEnd)
	path.FrontierTimeline, path.FrontierPosition, path.FrontierReceipt = current, frontier, receipt
	path.FrontierWAL, _ = WALName(current, frontier, wal.SegmentSize)
	if state != evidence.Healthy {
		path.State, path.Stop, path.Reason = state, stop, reason
		return path
	}
	position = requiredEnd
	for _, edge := range edges {
		frontier, receipt, state, stop, reason = reachPosition(wal, current, position, edge.SwitchPosition)
		path.FrontierTimeline, path.FrontierPosition, path.FrontierReceipt = current, frontier, receipt
		path.FrontierWAL, _ = WALName(current, frontier, wal.SegmentSize)
		if state != evidence.Healthy {
			path.State, path.Stop, path.Reason = state, stop, reason
			return path
		}
		if frontier < edge.SwitchPosition || !rangeContains(wal.Ranges, edge.Child, edge.SwitchPosition) {
			path.State, path.Stop, path.Reason = evidence.Unknown, CoverageUnknownLimited, "timeline switch segment is not observed on both timelines"
			return path
		}
		current, position = edge.Child, edge.SwitchPosition
	}
	frontier, receipt, state, stop, reason = contiguousFrontier(wal, current, position)
	path.FrontierTimeline, path.FrontierPosition, path.FrontierReceipt = current, frontier, receipt
	path.FrontierWAL, _ = WALName(current, frontier, wal.SegmentSize)
	path.State, path.Stop, path.Reason = state, stop, reason
	return path
}

func reachPosition(wal ServerWAL, timeline uint32, start, required uint64) (uint64, time.Time, evidence.State, CoverageStop, string) {
	ranges := rangesForTimeline(wal.Ranges, timeline)
	for index, value := range ranges {
		if start < value.Start {
			return start, time.Time{}, evidence.Unknown, CoverageUnknownLimited, "required starting WAL segment was not observed"
		}
		if start > value.End {
			continue
		}
		if value.End >= required {
			receipt := time.Time{}
			if value.End == required {
				receipt = value.EndReceipt
			}
			return required, receipt, evidence.Healthy, CoverageFrontier, ""
		}
		if index+1 < len(ranges) {
			gap := gapAfter(wal.Gaps, timeline, value.End)
			if gap.Status == GapConfirmed {
				return value.End, value.EndReceipt, evidence.Unhealthy, CoverageGapLimited, "confirmed required WAL gap stops coverage"
			}
			if gap.Status == GapCandidate {
				return value.End, value.EndReceipt, evidence.Warning, CoverageCandidateLimited, "candidate required WAL gap stops coverage"
			}
			return value.End, value.EndReceipt, evidence.Unknown, CoverageUnknownLimited, "unclassified WAL discontinuity stops coverage"
		}
		return value.End, value.EndReceipt, evidence.Unknown, CoverageUnknownLimited, "required backup consistency WAL was not observed"
	}
	return start, time.Time{}, evidence.Unknown, CoverageUnknownLimited, "required starting WAL segment was not observed"
}

func contiguousFrontier(wal ServerWAL, timeline uint32, start uint64) (uint64, time.Time, evidence.State, CoverageStop, string) {
	ranges := rangesForTimeline(wal.Ranges, timeline)
	for index, value := range ranges {
		if start < value.Start {
			return start, time.Time{}, evidence.Unknown, CoverageUnknownLimited, "required starting WAL segment was not observed"
		}
		if start > value.End {
			continue
		}
		if index+1 < len(ranges) {
			gap := gapAfter(wal.Gaps, timeline, value.End)
			if gap.Status == GapConfirmed {
				return value.End, value.EndReceipt, evidence.Unhealthy, CoverageGapLimited, "confirmed required WAL gap stops coverage"
			}
			if gap.Status == GapCandidate {
				return value.End, value.EndReceipt, evidence.Warning, CoverageCandidateLimited, "candidate required WAL gap stops coverage"
			}
			return value.End, value.EndReceipt, evidence.Unknown, CoverageUnknownLimited, "unclassified WAL discontinuity stops coverage"
		}
		return value.End, value.EndReceipt, evidence.Healthy, CoverageFrontier, "observed contiguous segment-name coverage reaches the current archive frontier"
	}
	return start, time.Time{}, evidence.Unknown, CoverageUnknownLimited, "required starting WAL segment was not observed"
}

func backupRange(backup Backup) (uint32, uint64, uint64, error) {
	beginTimeline, begin, err := ParseWALName(backup.BeginWAL, backup.SegmentSize)
	if err != nil {
		return 0, 0, 0, err
	}
	endTimeline, end, err := ParseWALName(backup.EndWAL, backup.SegmentSize)
	if err != nil || beginTimeline != endTimeline || end < begin || (backup.Timeline != 0 && backup.Timeline != beginTimeline) {
		return 0, 0, 0, errors.New("invalid backup WAL range")
	}
	return beginTimeline, begin, end, nil
}

func branchBeforeBackup(edge HistoryEdge, backup Backup, end uint64) bool {
	if backup.EndLSN != "" {
		endLSN, err := ParseLSN(backup.EndLSN)
		return err != nil || edge.SwitchLSN <= endLSN
	}
	return edge.SwitchPosition <= end
}

func descendantEdges(edges []HistoryEdge, start uint32) ([]HistoryEdge, bool) {
	for index, edge := range edges {
		if edge.Parent == start {
			return slices.Clone(edges[index:]), true
		}
	}
	return nil, false
}

func retentionSummary(backups []Backup, options RecoveryOptions) RetentionSummary {
	value := RetentionSummary{VisibleBackups: len(backups), PolicyConfigured: options.ExpectedRetentionPolicy != "", State: evidence.Healthy, Reason: "descriptive backup inventory; no retention expectation configured"}
	for _, backup := range backups {
		if backup.State == evidence.Healthy {
			value.StructurallyUsable++
		}
		if !backup.EndAt.IsZero() && (value.OldestCompletion.IsZero() || backup.EndAt.Before(value.OldestCompletion)) {
			value.OldestCompletion = backup.EndAt.UTC()
		}
		if backup.EndAt.After(value.NewestCompletion) {
			value.NewestCompletion = backup.EndAt.UTC()
		}
	}
	if options.ExpectedMinimumRedundancy != nil {
		value.MinimumConfigured, value.MinimumRedundancy = true, *options.ExpectedMinimumRedundancy
		if value.StructurallyUsable < value.MinimumRedundancy {
			value.State, value.Reason = evidence.Unhealthy, "structurally usable backup count is below configured minimum"
		} else {
			value.Reason = "structurally usable backup count meets configured minimum"
		}
	}
	if value.PolicyConfigured {
		value.State, value.Reason = evidence.Unknown, "retention policy syntax is not interpreted in this slice"
	}
	return value
}

func coverageRollup(paths []RecoveryPath) (evidence.State, string) {
	if len(paths) == 0 {
		return evidence.Unknown, "no structurally usable backup recovery anchors"
	}
	state := evidence.Healthy
	for _, path := range paths {
		state = conservativeRecoveryState(state, path.State)
	}
	switch state {
	case evidence.Unknown:
		return state, "one or more recovery paths contain unknown evidence"
	case evidence.Unhealthy:
		return state, "one or more recovery paths cross a confirmed required gap"
	case evidence.Warning:
		return state, "one or more recovery paths stop at a candidate required gap"
	default:
		return state, "all evaluated recovery paths reach an observed archive frontier"
	}
}

func conservativeRecoveryState(left, right evidence.State) evidence.State {
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

func historiesForServer(values []HistoryObject, server string) []HistoryObject {
	result := make([]HistoryObject, 0)
	for _, value := range values {
		if value.Server == server {
			result = append(result, value)
		}
	}
	return result
}

func backupsForServer(values []Backup, server string) []Backup {
	result := make([]Backup, 0)
	for _, value := range values {
		if value.Server == server {
			result = append(result, value)
		}
	}
	return result
}

func conflictingSystemIDs(backups []Backup) bool {
	known := ""
	for _, backup := range backups {
		if backup.State != evidence.Healthy || backup.SystemID == "" {
			continue
		}
		if known == "" {
			known = backup.SystemID
			continue
		}
		if known != backup.SystemID {
			return true
		}
	}
	return false
}

func recoveryAssumptions(backup Backup) []string {
	result := []string{"segment-name presence only", "timeline history metadata only", "WAL bytes and restore execution not verified"}
	if backup.SystemID == "" {
		return append(result, "backup system identifier unavailable")
	}
	return append(result, "backup system identifier retained; WAL objects do not carry it")
}

func observedTimelines(wal ServerWAL) map[uint32]struct{} {
	result := make(map[uint32]struct{})
	for _, value := range wal.Ranges {
		result[value.Timeline] = struct{}{}
	}
	return result
}

func historyEdgesAgree(edges []HistoryEdge, known map[uint32]HistoryEdge) bool {
	for _, edge := range edges {
		if prior, ok := known[edge.Child]; ok && prior != edge {
			return false
		}
	}
	for _, edge := range edges {
		known[edge.Child] = edge
	}
	return true
}

func cycleInEdges(edges map[uint32]HistoryEdge) bool {
	for child := range edges {
		seen := map[uint32]bool{}
		for current := child; current != 0; {
			if seen[current] {
				return true
			}
			seen[current] = true
			edge, ok := edges[current]
			if !ok {
				break
			}
			current = edge.Parent
		}
	}
	return false
}

func childRangesAgree(edges []HistoryEdge, ranges []WALRange) bool {
	for _, edge := range edges {
		child := rangesForTimeline(ranges, edge.Child)
		if len(child) > 0 && (child[0].Start < edge.SwitchPosition || !rangeContains(ranges, edge.Child, edge.SwitchPosition)) {
			return false
		}
	}
	return true
}

func rangesForTimeline(values []WALRange, timeline uint32) []WALRange {
	result := make([]WALRange, 0)
	for _, value := range values {
		if value.Timeline == timeline {
			result = append(result, value)
		}
	}
	return result
}

func rangeContains(values []WALRange, timeline uint32, position uint64) bool {
	for _, value := range values {
		if value.Timeline == timeline && value.Start <= position && position <= value.End {
			return true
		}
	}
	return false
}

func gapAfter(values []WALGap, timeline uint32, position uint64) WALGap {
	for _, value := range values {
		if value.Timeline == timeline && value.Start == position+1 {
			return value
		}
	}
	return WALGap{}
}

func unknownPath(backup Backup, reason string) RecoveryPath {
	return RecoveryPath{Server: backup.Server, BackupID: backup.ID, State: evidence.Unknown, Stop: CoverageUnknownLimited, Reason: reason, LowerBound: backup.EndAt.UTC(), Assumptions: recoveryAssumptions(backup)}
}

func (c RecoveryCatalog) Validate() error {
	if len(c.Servers) > 1_000 {
		return errors.New("Barman recovery catalog exceeds server limit")
	}
	for index, server := range c.Servers {
		if !validDiscoveredName(server.Server) || (index > 0 && c.Servers[index-1].Server >= server.Server) || !validEvidenceState(server.TimelineState) || !validEvidenceState(server.CoverageState) || !boundedWALText(server.TimelineReason, 256) || !boundedWALText(server.CoverageReason, 256) || len(server.Paths) > MaxRecoveryPaths || len(server.Histories) > MaxHistoryFiles {
			return errors.New("invalid Barman recovery server")
		}
		for historyIndex, history := range server.Histories {
			if history.Server != server.Server || history.Timeline < 2 || !validEvidenceState(history.State) || !boundedWALText(history.Reason, 256) || len(history.Edges) > MaxHistoryEntries {
				return errors.New("invalid Barman timeline history")
			}
			if historyIndex > 0 && server.Histories[historyIndex-1].Timeline >= history.Timeline {
				return errors.New("Barman timeline histories are not uniquely sorted")
			}
			if history.Key != "" && (len(history.Key) > store.MaxKeyBytes || strings.IndexFunc(history.Key, unicode.IsControl) >= 0) {
				return errors.New("invalid Barman timeline history key")
			}
			for _, edge := range history.Edges {
				if edge.Parent == 0 || edge.Child <= edge.Parent || edge.SwitchLSN == 0 || edge.SwitchWAL == "" || len(edge.SwitchWAL) != 24 || !isHex(edge.SwitchWAL) {
					return errors.New("invalid Barman timeline history edge")
				}
			}
		}
		for pathIndex, path := range server.Paths {
			if path.Server != server.Server || path.BackupID == "" || len(path.BackupID) > 256 || strings.IndexFunc(path.BackupID, unicode.IsControl) >= 0 || !validEvidenceState(path.State) || !boundedWALText(path.Reason, 256) || (path.Stop != CoverageFrontier && path.Stop != CoverageCandidateLimited && path.Stop != CoverageGapLimited && path.Stop != CoverageUnknownLimited) || !utcOrZero(path.LowerBound) || !utcOrZero(path.FrontierReceipt) || len(path.Assumptions) > 8 || !optionalWALName(path.StartWAL) || !optionalWALName(path.FrontierWAL) {
				return errors.New("invalid Barman recovery path")
			}
			if pathIndex > 0 && (server.Paths[pathIndex-1].BackupID > path.BackupID || (server.Paths[pathIndex-1].BackupID == path.BackupID && server.Paths[pathIndex-1].TargetTimeline >= path.TargetTimeline)) {
				return errors.New("Barman recovery paths are not uniquely sorted")
			}
			for _, assumption := range path.Assumptions {
				if !boundedWALText(assumption, 256) {
					return errors.New("invalid Barman recovery assumption")
				}
			}
		}
		retention := server.Retention
		if retention.VisibleBackups < 0 || retention.StructurallyUsable < 0 || retention.StructurallyUsable > retention.VisibleBackups || retention.MinimumRedundancy < 0 || (!retention.MinimumConfigured && retention.MinimumRedundancy != 0) || !validEvidenceState(retention.State) || !boundedWALText(retention.Reason, 256) || !utcOrZero(retention.OldestCompletion) || !utcOrZero(retention.NewestCompletion) || (!retention.OldestCompletion.IsZero() && !retention.NewestCompletion.IsZero() && retention.NewestCompletion.Before(retention.OldestCompletion)) {
			return errors.New("invalid Barman retention summary")
		}
	}
	return nil
}

func optionalWALName(value string) bool {
	return value == "" || (len(value) == 24 && isHex(value))
}

func CloneRecoveryCatalog(value RecoveryCatalog) RecoveryCatalog {
	result := RecoveryCatalog{Servers: make([]ServerRecovery, len(value.Servers))}
	for index, server := range value.Servers {
		result.Servers[index] = server
		result.Servers[index].Histories = make([]TimelineHistory, len(server.Histories))
		for historyIndex, history := range server.Histories {
			result.Servers[index].Histories[historyIndex] = history
			result.Servers[index].Histories[historyIndex].Edges = slices.Clone(history.Edges)
		}
		result.Servers[index].Paths = make([]RecoveryPath, len(server.Paths))
		for pathIndex, path := range server.Paths {
			result.Servers[index].Paths[pathIndex] = path
			result.Servers[index].Paths[pathIndex].Assumptions = slices.Clone(path.Assumptions)
		}
	}
	return result
}
