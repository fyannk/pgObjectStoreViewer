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

package cursor

import (
	"strings"
	"testing"
)

func TestCodecConfinesTokensToPrefixAndStore(t *testing.T) {
	t.Parallel()
	first, err := New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := New()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := first.Encode("alpha/", "provider-token-canary")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "" || encoded == "provider-token-canary" {
		t.Fatalf("opaque cursor = %q", encoded)
	}
	if got, err := first.Decode("alpha/", encoded); err != nil || got != "provider-token-canary" {
		t.Fatalf("Decode() = %q, %v", got, err)
	}
	for name, codec := range map[string]Codec{"other store": second, "tampered": first} {
		value := encoded
		if name == "tampered" {
			value = encoded[:len(encoded)-1] + "A"
		}
		if _, err := codec.Decode("beta/", value); err == nil {
			t.Fatalf("%s cursor was accepted", name)
		}
	}
}

func TestCodecRejectsOversizedCursor(t *testing.T) {
	t.Parallel()
	codec, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Encode("prefix", strings.Repeat("x", maxEncodedBytes)); err == nil {
		t.Fatal("oversized native token was accepted")
	}
	if _, err := codec.Decode("prefix", strings.Repeat("x", maxEncodedBytes+1)); err == nil {
		t.Fatal("oversized encoded cursor was accepted")
	}
}
