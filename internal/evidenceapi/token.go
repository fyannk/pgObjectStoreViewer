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
	"io"
	"os"
	"path/filepath"
)

const (
	decodedTokenBytes = 32
	encodedTokenBytes = 43
	maximumTokenFile  = 128
)

var ErrInvalidTokenFile = errors.New("evidence token file is invalid")

// Token retains the validated encoded bearer value without exposing a string
// or byte accessor.
type Token struct {
	encoded [encodedTokenBytes]byte
}

func (Token) String() string   { return "[REDACTED]" }
func (Token) GoString() string { return "[REDACTED]" }

func (t Token) valid() bool {
	return t != Token{}
}

// LoadTokenFile reads the fixed-size pod-local bearer token from one regular,
// non-symlink, group-readable Secret subPath file. Errors never include its
// path or contents.
func LoadTokenFile(path string) (Token, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Token{}, ErrInvalidTokenFile
	}
	before, err := os.Lstat(path)
	if err != nil || !validTokenMode(before.Mode()) {
		return Token{}, ErrInvalidTokenFile
	}
	file, err := os.Open(path)
	if err != nil {
		return Token{}, ErrInvalidTokenFile
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil || !validTokenMode(after.Mode()) || !os.SameFile(before, after) {
		return Token{}, ErrInvalidTokenFile
	}
	value, err := io.ReadAll(io.LimitReader(file, maximumTokenFile+1))
	if err != nil || len(value) > maximumTokenFile {
		return Token{}, ErrInvalidTokenFile
	}
	value = bytes.TrimSuffix(value, []byte{'\n'})
	return parseToken(value)
}

func validTokenMode(mode os.FileMode) bool {
	return mode.IsRegular() && mode.Perm() == 0o440 && mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
}

func parseToken(encoded []byte) (Token, error) {
	if len(encoded) != encodedTokenBytes {
		return Token{}, ErrInvalidTokenFile
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(encoded))
	if err != nil || len(decoded) != decodedTokenBytes || !bytes.Equal([]byte(base64.RawURLEncoding.EncodeToString(decoded)), encoded) {
		return Token{}, ErrInvalidTokenFile
	}
	var token Token
	copy(token.encoded[:], encoded)
	return token, nil
}
