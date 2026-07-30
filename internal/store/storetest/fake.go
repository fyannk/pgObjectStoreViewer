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

// Package storetest contains the deterministic read-store fake shared by
// provider-neutral and repository-format contract tests.
package storetest

import (
	"context"
	"io"
	"strings"

	"github.com/fyannk/pgObjectStoreViewer/internal/store"
)

// Fake delegates to optional functions and otherwise returns empty evidence.
type Fake struct {
	ListFunc func(context.Context, store.ListRequest) (store.Page, error)
	OpenFunc func(context.Context, store.OpenRequest) (io.ReadCloser, error)
	StatFunc func(context.Context, string) (store.Object, error)
}

func (f *Fake) List(ctx context.Context, request store.ListRequest) (store.Page, error) {
	if err := ctx.Err(); err != nil {
		return store.Page{}, err
	}
	if err := request.Validate(); err != nil {
		return store.Page{}, err
	}
	if f.ListFunc != nil {
		return f.ListFunc(ctx, request)
	}
	return store.Page{}, nil
}

func (f *Fake) Open(ctx context.Context, request store.OpenRequest) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if f.OpenFunc != nil {
		return f.OpenFunc(ctx, request)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *Fake) Stat(ctx context.Context, request store.StatRequest) (store.Object, error) {
	if err := ctx.Err(); err != nil {
		return store.Object{}, err
	}
	if err := request.Validate(); err != nil {
		return store.Object{}, err
	}
	if f.StatFunc != nil {
		return f.StatFunc(ctx, request.Key)
	}
	return store.Object{Key: request.Key}, nil
}

var _ store.Reader = (*Fake)(nil)
