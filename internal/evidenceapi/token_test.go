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

package evidenceapi

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTokenFileAcceptsOnlyCanonicalSecretSubPath(t *testing.T) {
	directory := t.TempDir()
	raw := bytes.Repeat([]byte{0x51}, decodedTokenBytes)
	encoded := base64.RawURLEncoding.EncodeToString(raw)

	for _, suffix := range []string{"", "\n"} {
		path := filepath.Join(directory, fmt.Sprintf("token-%d", len(suffix)))
		writeTokenFixture(t, path, []byte(encoded+suffix), 0o440)
		token, err := LoadTokenFile(path)
		if err != nil || string(token.encoded[:]) != encoded {
			t.Fatalf("loaded token = %s, %v", token, err)
		}
		if fmt.Sprintf("%s %#v", token, token) != "[REDACTED] [REDACTED]" {
			t.Fatal("token formatting was not redacted")
		}
	}
}

func TestLoadTokenFileRejectsUnsafeOrMalformedInputsWithoutDisclosure(t *testing.T) {
	directory := t.TempDir()
	encoded := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x52}, decodedTokenBytes))
	fixtures := []struct {
		name    string
		content []byte
		mode    os.FileMode
	}{
		{name: "empty", mode: 0o440},
		{name: "short", content: []byte(encoded[:42]), mode: 0o440},
		{name: "padded", content: []byte(encoded + "="), mode: 0o440},
		{name: "invalid alphabet", content: []byte(strings.Repeat("!", encodedTokenBytes)), mode: 0o440},
		{name: "CRLF", content: []byte(encoded + "\r\n"), mode: 0o440},
		{name: "two newlines", content: []byte(encoded + "\n\n"), mode: 0o440},
		{name: "oversized", content: bytes.Repeat([]byte{'A'}, maximumTokenFile+1), mode: 0o440},
		{name: "owner only", content: []byte(encoded), mode: 0o400},
		{name: "owner writable", content: []byte(encoded), mode: 0o640},
		{name: "world readable", content: []byte(encoded), mode: 0o444},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(directory, strings.ReplaceAll(fixture.name, " ", "-"))
			writeTokenFixture(t, path, fixture.content, fixture.mode)
			_, err := LoadTokenFile(path)
			if !errors.Is(err, ErrInvalidTokenFile) || strings.Contains(fmt.Sprint(err), path) || strings.Contains(fmt.Sprint(err), encoded) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	regular := filepath.Join(directory, "regular")
	writeTokenFixture(t, regular, []byte(encoded), 0o440)
	symlink := filepath.Join(directory, "symlink")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative-token", filepath.Join(directory, "missing"), directory, symlink, directory + "/sub/../regular"} {
		if _, err := LoadTokenFile(path); !errors.Is(err, ErrInvalidTokenFile) {
			t.Fatalf("path %q error = %v", path, err)
		}
	}
}

func writeTokenFixture(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
