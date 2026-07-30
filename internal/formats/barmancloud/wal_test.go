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
	"testing"
	"time"

	"github.com/fyannk/pgObjectStoreViewer/internal/evidence"
	"github.com/fyannk/pgObjectStoreViewer/internal/store"
)

func TestClassifyWALObject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, key, compression string
		class                  WALClass
		belongs                bool
	}{
		{name: "plain segment", key: "alpha/wals/0000000100000000/000000010000000000000001", class: WALSegment, compression: "none", belongs: true},
		{name: "gzip segment", key: "alpha/wals/0000000100000000/000000010000000000000001.gz", class: WALSegment, compression: "gzip", belongs: true},
		{name: "bzip2 segment", key: "alpha/wals/0000000100000000/000000010000000000000001.bz2", class: WALSegment, compression: "bzip2", belongs: true},
		{name: "snappy segment", key: "alpha/wals/0000000100000000/000000010000000000000001.snappy", class: WALSegment, compression: "snappy", belongs: true},
		{name: "lz4 segment", key: "alpha/wals/0000000100000000/000000010000000000000001.lz4", class: WALSegment, compression: "lz4", belongs: true},
		{name: "xz segment", key: "alpha/wals/0000000100000000/000000010000000000000001.xz", class: WALSegment, compression: "xz", belongs: true},
		{name: "zstd segment", key: "alpha/wals/0000000100000000/000000010000000000000001.zst", class: WALSegment, compression: "zstd", belongs: true},
		{name: "partial", key: "alpha/wals/0000000100000000/000000010000000000000001.partial.gz", class: WALPartial, compression: "gzip", belongs: true},
		{name: "timeline history", key: "alpha/wals/00000002.history.gz", class: WALHistory, compression: "gzip", belongs: true},
		{name: "backup history", key: "alpha/wals/0000000100000000/000000010000000000000001.00000028.backup.gz", class: WALBackupHistory, compression: "gzip", belongs: true},
		{name: "wrong hash", key: "alpha/wals/0000000100000001/000000010000000000000001", class: WALUnknown, belongs: true},
		{name: "segment at root", key: "alpha/wals/000000010000000000000001", class: WALUnknown, belongs: true},
		{name: "history under hash", key: "alpha/wals/0000000100000000/00000002.history", class: WALUnknown, belongs: true},
		{name: "malformed", key: "alpha/wals/0000000100000000/not-a-wal", class: WALUnknown, belongs: true},
		{name: "unsupported compression", key: "alpha/wals/0000000100000000/000000010000000000000001.zip", class: WALUnknown, belongs: true},
		{name: "backup object", key: "alpha/base/id/data.tar", belongs: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			classified, belongs := ClassifyWALObject(store.Object{Key: test.key})
			if belongs != test.belongs || (belongs && classified.Class != test.class) || (test.compression != "" && classified.Compression != test.compression) {
				t.Fatalf("ClassifyWALObject() = %#v, %t", classified, belongs)
			}
		})
	}
}

func TestWALArithmeticMatchesPostgreSQLSegmentRollover(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		segmentSize  int64
		log, segment uint32
		wantPosition uint64
		wantName     string
	}{
		{name: "one MiB", segmentSize: 1 << 20, log: 2, segment: 4095, wantPosition: 12287, wantName: "000000010000000200000FFF"},
		{name: "sixteen MiB", segmentSize: 16 << 20, log: 2, segment: 255, wantPosition: 767, wantName: "0000000100000002000000FF"},
		{name: "one GiB", segmentSize: 1 << 30, log: 2, segment: 3, wantPosition: 11, wantName: "000000010000000200000003"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			position, err := WALPosition(1, test.log, test.segment, test.segmentSize)
			if err != nil || position != test.wantPosition {
				t.Fatalf("WALPosition() = %d, %v; want %d", position, err, test.wantPosition)
			}
			name, err := WALName(1, position, test.segmentSize)
			if err != nil || name != test.wantName {
				t.Fatalf("WALName() = %q, %v; want %q", name, err, test.wantName)
			}
			next, err := WALName(1, position+1, test.segmentSize)
			if err != nil || next != "000000010000000300000000" {
				t.Fatalf("rollover WALName() = %q, %v", next, err)
			}
		})
	}
	for _, invalid := range []int64{0, 512 << 10, 3 << 20, 2 << 30} {
		if ValidWALSegmentSize(invalid) {
			t.Errorf("ValidWALSegmentSize(%d) = true", invalid)
		}
	}
	if _, err := WALPosition(1, 0, 4, 1<<30); err == nil {
		t.Fatal("one-GiB segment component 4 was accepted")
	}
}

func TestWALCollectorUsesCompactRangesAndTwoCompleteScanGapLifecycle(t *testing.T) {
	t.Parallel()
	backup := Backup{Server: "alpha", PostgreSQLVersion: 180000, SegmentSize: 16 << 20}
	first := NewWALCollector()
	for _, position := range []uint64{1, 2, 5} {
		first.Add(testWALObject(t, "alpha", 1, position, backup.SegmentSize, ""))
	}
	firstCatalog := first.Finish([]Backup{backup}, WALCatalog{}, 1)
	server := firstCatalog.Servers[0]
	if server.State != evidence.Warning || len(server.Ranges) != 2 || len(server.Gaps) != 1 || server.Gaps[0].Status != GapCandidate || server.Gaps[0].Count != 2 {
		t.Fatalf("first complete scan = %#v", server)
	}
	second := NewWALCollector()
	for _, position := range []uint64{1, 2, 5} {
		second.Add(testWALObject(t, "alpha", 1, position, backup.SegmentSize, ""))
	}
	secondCatalog := second.Finish([]Backup{backup}, firstCatalog, 3)
	server = secondCatalog.Servers[0]
	if server.State != evidence.Unhealthy || server.Gaps[0].Status != GapConfirmed || server.Gaps[0].FirstObservedGeneration != 1 || server.Gaps[0].LastObservedGeneration != 3 {
		t.Fatalf("second complete scan = %#v", server)
	}
	cleared := NewWALCollector()
	for _, position := range []uint64{1, 2, 3, 4, 5} {
		cleared.Add(testWALObject(t, "alpha", 1, position, backup.SegmentSize, ""))
	}
	clearedCatalog := cleared.Finish([]Backup{backup}, secondCatalog, 4)
	server = clearedCatalog.Servers[0]
	if server.State != evidence.Healthy || len(server.Ranges) != 1 || len(server.Gaps) != 0 {
		t.Fatalf("complete scan that filled gap = %#v", server)
	}
}

func TestWALCollectorKeepsHugeGapsCompactAndRejectsDuplicates(t *testing.T) {
	t.Parallel()
	const segmentSize = int64(1 << 20)
	backup := Backup{Server: "alpha", PostgreSQLVersion: 180000, SegmentSize: segmentSize}
	collector := NewWALCollector()
	collector.Add(testWALObject(t, "alpha", 1, 1, segmentSize, ""))
	collector.Add(testWALObject(t, "alpha", 1, 1_000_000, segmentSize, ""))
	catalog := collector.Finish([]Backup{backup}, WALCatalog{}, 1)
	server := catalog.Servers[0]
	if len(server.Ranges) != 2 || len(server.Gaps) != 1 || server.Gaps[0].Count != 999_998 {
		t.Fatalf("huge gap was not compact: %#v", server)
	}

	duplicates := NewWALCollector()
	duplicates.Add(testWALObject(t, "alpha", 1, 7, segmentSize, ""))
	duplicates.Add(testWALObject(t, "alpha", 1, 7, segmentSize, ".gz"))
	server = duplicates.Finish([]Backup{backup}, WALCatalog{}, 1).Servers[0]
	if server.State != evidence.Unknown || server.Counts.Segments != 1 || server.Counts.Duplicates != 1 || len(server.Diagnostics) != 1 || server.Diagnostics[0].Class != WALDuplicate {
		t.Fatalf("duplicate evidence = %#v", server)
	}

	malformed := NewWALCollector()
	malformed.Add(testWALObject(t, "alpha", 1, 7, segmentSize, ""))
	malformed.Add(store.Object{Key: "alpha/wals/0000000100000000/not-a-wal"})
	server = malformed.Finish([]Backup{backup}, WALCatalog{}, 1).Servers[0]
	if server.State != evidence.Unknown || server.Counts.Unknown != 1 {
		t.Fatalf("malformed evidence = %#v", server)
	}
}

func TestWALCollectorFailsUnknownWithoutConsistentMetadataContext(t *testing.T) {
	t.Parallel()
	collector := NewWALCollector()
	collector.Add(testWALObject(t, "alpha", 1, 1, 16<<20, ""))
	tests := []struct {
		name    string
		backups []Backup
	}{
		{name: "missing context", backups: []Backup{{Server: "alpha"}}},
		{name: "invalid segment size", backups: []Backup{{Server: "alpha", PostgreSQLVersion: 180000, SegmentSize: 3 << 20}}},
		{name: "conflicting context", backups: []Backup{{Server: "alpha", PostgreSQLVersion: 170000, SegmentSize: 16 << 20}, {Server: "alpha", PostgreSQLVersion: 180000, SegmentSize: 16 << 20}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := collector.Finish(test.backups, WALCatalog{}, 1).Servers[0]
			if server.State != evidence.Unknown {
				t.Fatalf("server = %#v", server)
			}
		})
	}
}

func TestWALDiagnosticTruncationForcesUnknown(t *testing.T) {
	t.Parallel()
	backup := Backup{Server: "alpha", PostgreSQLVersion: 180000, SegmentSize: 16 << 20}
	collector := NewWALCollector()
	collector.Add(testWALObject(t, "alpha", 1, 1, backup.SegmentSize, ""))
	for position := uint64(2); position < uint64(MaxWALDiagnostics)+3; position++ {
		collector.Add(testWALObject(t, "alpha", 1, position, backup.SegmentSize, ".partial"))
	}
	server := collector.Finish([]Backup{backup}, WALCatalog{}, 1).Servers[0]
	if !server.DiagnosticsTruncated || server.State != evidence.Unknown {
		t.Fatalf("truncated diagnostics = %#v", server)
	}
}

func testWALObject(t *testing.T, server string, timeline uint32, position uint64, segmentSize int64, suffix string) store.Object {
	t.Helper()
	name, err := WALName(timeline, position, segmentSize)
	if err != nil {
		t.Fatal(err)
	}
	return store.Object{
		Key:  server + "/wals/" + name[:16] + "/" + name + suffix,
		Size: segmentSize, LastModified: time.Unix(int64(position), 0).UTC(),
	}
}
