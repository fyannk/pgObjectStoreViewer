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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/ulikunitz/xz"

	"github.com/fyannk/pgObjectStoreViewer/internal/evidence"
	"github.com/fyannk/pgObjectStoreViewer/internal/store"
	"github.com/fyannk/pgObjectStoreViewer/internal/store/storetest"
)

const validHistory = "1\t0/02000000\tfirst branch\n2\t0/04000000\tsecond branch\n"

func TestTimelineHistoryParserUsesExactPostgreSQLSwitchpoints(t *testing.T) {
	t.Parallel()
	edges, err := ParseHistory([]byte(validHistory), 3, 16<<20)
	if err != nil || len(edges) != 2 || edges[0].Parent != 1 || edges[0].Child != 2 || edges[0].SwitchPosition != 2 || edges[1].Parent != 2 || edges[1].Child != 3 || edges[1].SwitchPosition != 4 {
		t.Fatalf("ParseHistory() = %#v, %v", edges, err)
	}
	for _, malformed := range []string{"", "# only\n", "1 0/2 reason\n", "1\tbad\treason\n", "2\t0/2\ta\n1\t0/4\tb\n", "1\t0/4\ta\n2\t0/2\tb\n", "3\t0/2\tcycle\n"} {
		if _, err := ParseHistory([]byte(malformed), 3, 16<<20); err == nil {
			t.Errorf("ParseHistory(%q) error = nil", malformed)
		}
	}
	cycle := map[uint32]HistoryEdge{2: {Parent: 3, Child: 2}, 3: {Parent: 2, Child: 3}}
	if !cycleInEdges(cycle) {
		t.Fatal("cycle was not detected")
	}
}

func TestTimelineHistoryReaderSupportsEveryBarmanCloudCodec(t *testing.T) {
	t.Parallel()
	plain := []byte("1\t0/2000000\tbranch\n")
	encoders := map[string]func(*testing.T) []byte{
		"none": func(*testing.T) []byte { return plain },
		"gzip": func(t *testing.T) []byte {
			return encodeWithCloser(t, plain, func(buffer *bytes.Buffer) io.WriteCloser { return gzip.NewWriter(buffer) })
		},
		"bzip2": func(t *testing.T) []byte {
			value, err := base64.StdEncoding.DecodeString("QlpoOTFBWSZTWSY7m4EAAAVZgBAwAADwADhBEAAgACIAA0IBoAeF1idCqaEAcPF3JFOFCQJjubgQ")
			if err != nil {
				t.Fatal(err)
			}
			return value
		},
		"snappy": func(t *testing.T) []byte {
			return encodeWithCloser(t, plain, func(buffer *bytes.Buffer) io.WriteCloser { return snappy.NewBufferedWriter(buffer) })
		},
		"lz4": func(t *testing.T) []byte {
			return encodeWithCloser(t, plain, func(buffer *bytes.Buffer) io.WriteCloser { return lz4.NewWriter(buffer) })
		},
		"xz": func(t *testing.T) []byte {
			return encodeWithErrorCloser(t, plain, func(buffer *bytes.Buffer) (io.WriteCloser, error) { return xz.NewWriter(buffer) })
		},
		"zstd": func(t *testing.T) []byte {
			return encodeWithErrorCloser(t, plain, func(buffer *bytes.Buffer) (io.WriteCloser, error) { return zstd.NewWriter(buffer) })
		},
	}
	for compression, encode := range encoders {
		t.Run(compression, func(t *testing.T) {
			t.Parallel()
			reader, closer, err := historyReader(bytes.NewReader(encode(t)), compression)
			if err != nil {
				t.Fatal(err)
			}
			if closer != nil {
				defer closer.Close()
			}
			decoded, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(decoded, plain) {
				t.Fatalf("decoded = %q, %v", decoded, err)
			}
		})
	}
}

func TestTimelineHistoryBoundsFailUnknown(t *testing.T) {
	t.Parallel()
	oversized := bytes.Repeat([]byte("x"), int(MaxHistoryObjectBytes)+1)
	reader := &storetest.Fake{OpenFunc: func(_ context.Context, request store.OpenRequest) (io.ReadCloser, error) {
		if request.MaxBytes != MaxHistoryObjectBytes {
			t.Fatalf("Open MaxBytes = %d", request.MaxBytes)
		}
		return io.NopCloser(bytes.NewReader(oversized)), nil
	}}
	if _, err := readHistory(context.Background(), reader, HistoryObject{Key: "alpha/wals/00000002.history", Compression: "none"}); err == nil {
		t.Fatal("oversized compressed history was accepted")
	}

	decompressed := bytes.Repeat([]byte("x"), int(MaxHistoryDecompressedBytes)+1)
	compressed := encodeWithCloser(t, decompressed, func(buffer *bytes.Buffer) io.WriteCloser { return gzip.NewWriter(buffer) })
	reader = &storetest.Fake{OpenFunc: func(context.Context, store.OpenRequest) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(compressed)), nil
	}}
	if _, err := readHistory(context.Background(), reader, HistoryObject{Key: "alpha/wals/00000002.history.gz", Compression: "gzip"}); err == nil {
		t.Fatal("oversized decompressed history was accepted")
	}

	var entries strings.Builder
	for index := 1; index <= MaxHistoryEntries+1; index++ {
		_, _ = fmt.Fprintf(&entries, "%d\t0/%08X\tbranch\n", index, index)
	}
	if _, err := ParseHistory([]byte(entries.String()), MaxHistoryEntries+2, 16<<20); err == nil {
		t.Fatal("history entry ceiling was not enforced")
	}

	backup := recoveryBackup(time.Now().UTC(), 1, 1)
	catalog := AnalyzeRecovery(context.Background(), &storetest.Fake{}, []Backup{backup}, WALCatalog{Servers: []ServerWAL{recoveryWAL(GapCandidate)}}, nil, true, RecoveryOptions{})
	if catalog.Servers[0].TimelineState != evidence.Unknown {
		t.Fatalf("truncated history catalog = %#v", catalog.Servers[0])
	}
}

func TestRecoveryCoverageTraversesValidHistoryAndStopsAtRelevantGaps(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	backup := recoveryBackup(now, 1, 1)
	wal := recoveryWAL(GapCandidate)
	reader := historyFake(validHistory)
	catalog := AnalyzeRecovery(context.Background(), reader, []Backup{backup}, WALCatalog{Servers: []ServerWAL{wal}}, []HistoryObject{{Key: "alpha/wals/00000003.history", Server: "alpha", Name: "00000003.history", Compression: "none", Timeline: 3}}, false, RecoveryOptions{})
	server := catalog.Servers[0]
	if server.TimelineState != evidence.Healthy || len(server.Paths) != 3 {
		t.Fatalf("valid graph = %#v", server)
	}
	path := findPath(t, server.Paths, 3)
	if path.State != evidence.Healthy || path.FrontierTimeline != 3 || path.FrontierPosition != 6 || path.Stop != CoverageFrontier {
		t.Fatalf("descendant path = %#v", path)
	}
	path = findPath(t, server.Paths, 1)
	if path.State != evidence.Warning || path.FrontierPosition != 2 || path.Stop != CoverageCandidateLimited {
		t.Fatalf("candidate path = %#v", path)
	}
	wal.Gaps[0].Status = GapConfirmed
	catalog = AnalyzeRecovery(context.Background(), reader, []Backup{backup}, WALCatalog{Servers: []ServerWAL{wal}}, []HistoryObject{{Key: "alpha/wals/00000003.history", Server: "alpha", Name: "00000003.history", Compression: "none", Timeline: 3}}, false, RecoveryOptions{})
	path = findPath(t, catalog.Servers[0].Paths, 1)
	if path.State != evidence.Unhealthy || path.Stop != CoverageGapLimited {
		t.Fatalf("confirmed path = %#v", path)
	}
}

func TestRecoveryCoverageIgnoresHistoricalGapBeforeAnchor(t *testing.T) {
	t.Parallel()
	backup := recoveryBackup(time.Now().UTC(), 3, 3)
	wal := ServerWAL{Server: "alpha", State: evidence.Warning, PostgreSQLVersion: 180000, SegmentSize: 16 << 20,
		Ranges: []WALRange{walRange(1, 1, 1), walRange(1, 3, 5)},
		Gaps:   []WALGap{{Timeline: 1, Start: 2, End: 2, Count: 1, Status: GapCandidate, FirstObservedGeneration: 1, LastObservedGeneration: 1}},
	}
	catalog := AnalyzeRecovery(context.Background(), &storetest.Fake{}, []Backup{backup}, WALCatalog{Servers: []ServerWAL{wal}}, nil, false, RecoveryOptions{})
	path := catalog.Servers[0].Paths[0]
	if path.State != evidence.Healthy || path.FrontierPosition != 5 {
		t.Fatalf("historical gap invalidated path: %#v", path)
	}
}

func TestTimelineAndCoverageFailUnknownForMissingMalformedAndInconsistentEvidence(t *testing.T) {
	t.Parallel()
	backup := recoveryBackup(time.Now().UTC(), 1, 1)
	wal := recoveryWAL(GapCandidate)
	tests := []struct {
		name    string
		objects []HistoryObject
		reader  store.Reader
		mutate  func(*ServerWAL)
	}{
		{name: "missing history", reader: &storetest.Fake{}},
		{name: "malformed history", objects: []HistoryObject{{Key: "alpha/wals/00000003.history", Server: "alpha", Compression: "none", Timeline: 3}}, reader: historyFake("malformed")},
		{name: "child precedes switch", objects: []HistoryObject{{Key: "alpha/wals/00000003.history", Server: "alpha", Compression: "none", Timeline: 3}}, reader: historyFake(validHistory), mutate: func(value *ServerWAL) {
			value.Ranges = append(value.Ranges, walRange(3, 1, 1))
			sortRanges(value.Ranges)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := wal
			value.Ranges = append([]WALRange(nil), wal.Ranges...)
			if test.mutate != nil {
				test.mutate(&value)
			}
			catalog := AnalyzeRecovery(context.Background(), test.reader, []Backup{backup}, WALCatalog{Servers: []ServerWAL{value}}, test.objects, false, RecoveryOptions{})
			if catalog.Servers[0].TimelineState != evidence.Unknown || catalog.Servers[0].CoverageState != evidence.Unknown {
				t.Fatalf("timeline = %#v", catalog.Servers[0])
			}
		})
	}
	missingEnd := recoveryBackup(time.Now().UTC(), 1, 7)
	missingWAL := wal
	missingWAL.Ranges = []WALRange{walRange(1, 1, 1)}
	missingWAL.Gaps = nil
	catalog := AnalyzeRecovery(context.Background(), &storetest.Fake{}, []Backup{missingEnd}, WALCatalog{Servers: []ServerWAL{missingWAL}}, nil, false, RecoveryOptions{})
	if catalog.Servers[0].Paths[0].State != evidence.Unknown {
		t.Fatalf("missing consistency range = %#v", catalog.Servers[0].Paths[0])
	}
}

func TestBarmanRetentionIsDescriptiveUnlessExpectationConfigured(t *testing.T) {
	t.Parallel()
	backups := []Backup{{State: evidence.Healthy, EndAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)}, {State: evidence.Unhealthy, EndAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)}}
	value := retentionSummary(backups, RecoveryOptions{})
	if value.State != evidence.Healthy || value.StructurallyUsable != 1 {
		t.Fatalf("descriptive = %#v", value)
	}
	minimum := 2
	value = retentionSummary(backups, RecoveryOptions{ExpectedMinimumRedundancy: &minimum})
	if value.State != evidence.Unhealthy {
		t.Fatalf("minimum = %#v", value)
	}
	value = retentionSummary(backups, RecoveryOptions{ExpectedRetentionPolicy: "RECOVERY WINDOW OF 7 DAYS"})
	if value.State != evidence.Unknown {
		t.Fatalf("uninterpreted policy = %#v", value)
	}
}

func TestRecoveryCoverageDoesNotMergeConflictingSystemIdentifiers(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	first := recoveryBackup(now, 1, 1)
	first.ID, first.SystemID = "first", "7612345678901234567"
	second := recoveryBackup(now, 1, 1)
	second.ID, second.SystemID = "second", "7612345678901234568"
	catalog := AnalyzeRecovery(context.Background(), &storetest.Fake{}, []Backup{first, second}, WALCatalog{Servers: []ServerWAL{recoveryWAL(GapCandidate)}}, nil, false, RecoveryOptions{})
	if catalog.Servers[0].CoverageState != evidence.Unknown || len(catalog.Servers[0].Paths) != 2 {
		t.Fatalf("conflicting system identifiers = %#v", catalog.Servers[0])
	}
	for _, path := range catalog.Servers[0].Paths {
		if path.State != evidence.Unknown || !strings.Contains(path.Reason, "system identifiers") {
			t.Fatalf("path = %#v", path)
		}
	}
}

func recoveryBackup(now time.Time, begin, end uint64) Backup {
	beginName, _ := WALName(1, begin, 16<<20)
	endName, _ := WALName(1, end, 16<<20)
	return Backup{Server: "alpha", ID: "backup", State: evidence.Healthy, BeginWAL: beginName, EndWAL: endName, SegmentSize: 16 << 20, PostgreSQLVersion: 180000, Timeline: 1, EndAt: now}
}

func recoveryWAL(status GapStatus) ServerWAL {
	return ServerWAL{Server: "alpha", State: evidence.Warning, PostgreSQLVersion: 180000, SegmentSize: 16 << 20,
		Ranges: []WALRange{walRange(1, 1, 2), walRange(1, 4, 4), walRange(2, 2, 4), walRange(3, 4, 6)},
		Gaps:   []WALGap{{Timeline: 1, Start: 3, End: 3, Count: 1, First: "000000010000000000000003", Last: "000000010000000000000003", Status: status, FirstObservedGeneration: 1, LastObservedGeneration: map[bool]uint64{true: 2, false: 1}[status == GapConfirmed]}},
	}
}

func walRange(timeline uint32, start, end uint64) WALRange {
	first, _ := WALName(timeline, start, 16<<20)
	last, _ := WALName(timeline, end, 16<<20)
	return WALRange{Timeline: timeline, Start: start, End: end, Count: end - start + 1, First: first, Last: last, EndReceipt: time.Unix(int64(end), 0).UTC(), LatestReceipt: time.Unix(int64(end), 0).UTC()}
}

func historyFake(content string) store.Reader {
	return &storetest.Fake{OpenFunc: func(context.Context, store.OpenRequest) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(content))), nil
	}}
}

func findPath(t *testing.T, paths []RecoveryPath, timeline uint32) RecoveryPath {
	t.Helper()
	for _, path := range paths {
		if path.TargetTimeline == timeline {
			return path
		}
	}
	t.Fatalf("timeline %d path missing: %#v", timeline, paths)
	return RecoveryPath{}
}

func sortRanges(values []WALRange) {
	slices.SortFunc(values, func(a, b WALRange) int {
		if a.Timeline < b.Timeline {
			return -1
		}
		if a.Timeline > b.Timeline {
			return 1
		}
		if a.Start < b.Start {
			return -1
		}
		return 1
	})
}

func encodeWithCloser(t *testing.T, data []byte, create func(*bytes.Buffer) io.WriteCloser) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := create(&buffer)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func encodeWithErrorCloser(t *testing.T, data []byte, create func(*bytes.Buffer) (io.WriteCloser, error)) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer, err := create(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
