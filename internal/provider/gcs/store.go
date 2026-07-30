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

// Package gcs implements the read-only Google Cloud Storage boundary.
package gcs

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/fyannk/pgObjectStoreViewer/internal/fault"
	"github.com/fyannk/pgObjectStoreViewer/internal/provider/cursor"
	"github.com/fyannk/pgObjectStoreViewer/internal/store"
)

type Options struct {
	Bucket, Prefix  string
	Endpoint        string
	CredentialsJSON []byte
	RequestTimeout  time.Duration
}
type Store struct {
	client         *storage.Client
	bucket, prefix string
	timeout        time.Duration
	cursor         cursor.Codec
}

func New(ctx context.Context, options Options) (*Store, error) {
	if options.Bucket == "" || options.RequestTimeout <= 0 || options.RequestTimeout > 5*time.Minute {
		return nil, &Error{fault.InvalidConfig}
	}
	// Keep list, stat, and bounded object reads on one JSON API endpoint. This
	// is explicitly supported by the SDK and avoids the separate XML read host.
	clientOptions := []option.ClientOption{storage.WithJSONReads()}
	if options.Endpoint != "" {
		clientOptions = append(clientOptions, option.WithEndpoint(options.Endpoint), option.WithoutAuthentication())
	}
	if len(options.CredentialsJSON) > 0 {
		clientOptions = append(clientOptions, option.WithCredentialsJSON(options.CredentialsJSON))
	}
	client, err := storage.NewClient(ctx, clientOptions...)
	if err != nil {
		return nil, &Error{fault.InvalidConfig}
	}
	prefix := strings.Trim(options.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	codec, err := cursor.New()
	if err != nil {
		return nil, &Error{fault.Unavailable}
	}
	return &Store{client: client, bucket: options.Bucket, prefix: prefix, timeout: options.RequestTimeout, cursor: codec}, nil
}

func (s *Store) List(ctx context.Context, request store.ListRequest) (store.Page, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Prefix, "/") {
		return store.Page{}, store.ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	it := s.client.Bucket(s.bucket).Objects(ctx, &storage.Query{Prefix: s.prefix + request.Prefix, Projection: storage.ProjectionNoACL})
	nativeCursor := ""
	if request.Cursor != "" {
		token, err := s.cursor.Decode(request.Prefix, request.Cursor)
		if err != nil {
			return store.Page{}, store.ErrInvalidRequest
		}
		nativeCursor = token
	}
	pager := iterator.NewPager(it, request.Limit, nativeCursor)
	var objects []*storage.ObjectAttrs
	nextCursor, err := pager.NextPage(&objects)
	if err != nil {
		return store.Page{}, safeError(ctx, err)
	}
	if len(objects) > request.Limit {
		return store.Page{}, &Error{fault.Unavailable}
	}
	result := store.Page{Objects: make([]store.Object, 0, len(objects))}
	for _, attrs := range objects {
		key, ok := s.relative(attrs.Name, request.Prefix)
		if !ok || key == "" || len(key) > store.MaxKeyBytes || attrs.Size < 0 {
			return store.Page{}, &Error{fault.SafetyLimit}
		}
		etag := attrs.Etag
		if len(etag) > 512 {
			etag = ""
		}
		result.Objects = append(result.Objects, store.Object{Key: key, Size: attrs.Size, LastModified: attrs.Updated.UTC(), ETag: etag})
	}
	if nextCursor != "" {
		encoded, err := s.cursor.Encode(request.Prefix, nextCursor)
		if err != nil {
			return store.Page{}, &Error{fault.SafetyLimit}
		}
		result.NextCursor = encoded
	}
	return result, nil
}
func (s *Store) Open(ctx context.Context, request store.OpenRequest) (io.ReadCloser, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Key, "/") {
		return nil, store.ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	reader, err := s.client.Bucket(s.bucket).Object(s.prefix + request.Key).NewReader(ctx)
	if err != nil {
		cancel()
		return nil, safeError(ctx, err)
	}
	return &body{Reader: io.LimitReader(reader, request.MaxBytes), body: reader, cancel: cancel}, nil
}
func (s *Store) Stat(ctx context.Context, request store.StatRequest) (store.Object, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Key, "/") {
		return store.Object{}, store.ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	attrs, err := s.client.Bucket(s.bucket).Object(s.prefix + request.Key).Attrs(ctx)
	if err != nil {
		return store.Object{}, safeError(ctx, err)
	}
	if attrs.Size < 0 {
		return store.Object{}, &Error{fault.Unavailable}
	}
	etag := attrs.Etag
	if len(etag) > 512 {
		etag = ""
	}
	return store.Object{Key: request.Key, Size: attrs.Size, LastModified: attrs.Updated.UTC(), ETag: etag}, nil
}
func (s *Store) relative(full, requested string) (string, bool) {
	if !strings.HasPrefix(full, s.prefix) {
		return "", false
	}
	key := strings.TrimPrefix(full, s.prefix)
	return key, strings.HasPrefix(key, requested)
}

type body struct {
	io.Reader
	body   io.ReadCloser
	cancel context.CancelFunc
}

func (b *body) Close() error { b.cancel(); return b.body.Close() }

type Error struct{ kind fault.Category }

func (e *Error) Error() string            { return "gcs request failed: " + string(e.kind) }
func (e *Error) Category() fault.Category { return e.kind }
func safeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var apiError *googleapi.Error
	if errors.As(err, &apiError) {
		switch apiError.Code {
		case 401:
			return &Error{fault.Authentication}
		case 403:
			return &Error{fault.Authorization}
		case 404:
			return &Error{fault.NotFound}
		case 429:
			return &Error{fault.Throttled}
		}
	}
	return &Error{fault.Unavailable}
}

var _ store.Reader = (*Store)(nil)
