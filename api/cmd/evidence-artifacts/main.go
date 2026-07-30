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

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fyannk/objectstoreviewer/api/internal/evidenceartifacts"
)

func main() {
	check := flag.Bool("check", false, "fail when committed artifacts differ")
	output := flag.String("output", "evidence/v1alpha1", "artifact output directory")
	flag.Parse()
	if err := run(*output, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(output string, check bool) error {
	artifacts, err := evidenceartifacts.Generate()
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		path := filepath.Join(output, filepath.FromSlash(artifact.Path))
		if check {
			committed, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("check generated artifact %s: %w", artifact.Path, err)
			}
			if !bytes.Equal(committed, artifact.Data) {
				return fmt.Errorf("generated artifact %s is stale; run make generate-evidence-artifacts", artifact.Path)
			}
			continue
		}
		// #nosec G301,G306 -- this generator writes committed source artifacts
		// (schema.json and wire goldens) into the working tree. They are meant to
		// be world-readable like every other checked-in file, not secrets.
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create artifact directory for %s: %w", artifact.Path, err)
		}
		// #nosec G306 -- see the note above: committed generated source file.
		if err := os.WriteFile(path, artifact.Data, 0o644); err != nil {
			return fmt.Errorf("write generated artifact %s: %w", artifact.Path, err)
		}
	}
	return nil
}
