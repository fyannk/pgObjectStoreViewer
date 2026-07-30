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

// Package azure implements the read-only Blob Storage boundary. Azure SDK
// types do not leave this package.
package azure

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"github.com/fyannk/objectstoreviewer/internal/fault"
	"github.com/fyannk/objectstoreviewer/internal/provider/cursor"
	"github.com/fyannk/objectstoreviewer/internal/store"
)

type Options struct {
	Container, Prefix, Account, ConnectionString string
	AccountKey, SASToken                         []byte
	RequestTimeout                               time.Duration
}
type Store struct {
	client            *azblob.Client
	container, prefix string
	timeout           time.Duration
	cursor            cursor.Codec
}

func New(ctx context.Context, options Options) (*Store, error) {
	if options.Container == "" || options.RequestTimeout <= 0 || options.RequestTimeout > 5*time.Minute {
		return nil, &Error{fault.InvalidConfig}
	}
	var client *azblob.Client
	var err error
	switch {
	case options.ConnectionString != "":
		client, err = azblob.NewClientFromConnectionString(options.ConnectionString, nil)
	case options.Account != "" && len(options.AccountKey) > 0:
		credential, credentialErr := azblob.NewSharedKeyCredential(options.Account, string(options.AccountKey))
		if credentialErr != nil {
			return nil, &Error{fault.InvalidConfig}
		}
		client, err = azblob.NewClientWithSharedKeyCredential("https://"+options.Account+".blob.core.windows.net/", credential, nil)
	case options.Account != "" && len(options.SASToken) > 0:
		client, err = azblob.NewClientWithNoCredential("https://"+options.Account+".blob.core.windows.net/?"+strings.TrimPrefix(string(options.SASToken), "?"), nil)
	default:
		credential, credentialErr := azidentity.NewDefaultAzureCredential(nil)
		if credentialErr != nil {
			return nil, &Error{fault.InvalidConfig}
		}
		account := options.Account
		if account == "" {
			return nil, &Error{fault.InvalidConfig}
		}
		client, err = azblob.NewClient("https://"+account+".blob.core.windows.net/", credential, nil)
	}
	if err != nil {
		return nil, &Error{fault.InvalidConfig}
	}
	codec, err := cursor.New()
	if err != nil {
		return nil, &Error{fault.Unavailable}
	}
	return &Store{client: client, container: options.Container, prefix: rooted(options.Prefix), timeout: options.RequestTimeout, cursor: codec}, nil
}

func (s *Store) List(ctx context.Context, request store.ListRequest) (store.Page, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Prefix, "/") {
		return store.Page{}, store.ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	prefix := s.prefix + request.Prefix
	// #nosec G115 -- ListRequest.Validate bounds Limit to 1..store.MaxPageObjects (1000).
	options := &azblob.ListBlobsFlatOptions{Prefix: &prefix, MaxResults: ptr(int32(request.Limit))}
	if request.Cursor != "" {
		marker, err := s.cursor.Decode(request.Prefix, request.Cursor)
		if err != nil {
			return store.Page{}, store.ErrInvalidRequest
		}
		options.Marker = &marker
	}
	pager := s.client.NewListBlobsFlatPager(s.container, options)
	response, err := pager.NextPage(ctx)
	if err != nil {
		return store.Page{}, safeError(ctx, err)
	}
	if response.Segment == nil || len(response.Segment.BlobItems) > request.Limit {
		return store.Page{}, &Error{fault.Unavailable}
	}
	page := store.Page{Objects: make([]store.Object, 0, len(response.Segment.BlobItems))}
	for _, item := range response.Segment.BlobItems {
		if item.Name == nil || item.Properties.ContentLength == nil || *item.Properties.ContentLength < 0 {
			return store.Page{}, &Error{fault.Unavailable}
		}
		key, ok := s.relative(*item.Name, request.Prefix)
		if !ok || key == "" || len(key) > store.MaxKeyBytes {
			return store.Page{}, &Error{fault.SafetyLimit}
		}
		object := store.Object{Key: key, Size: *item.Properties.ContentLength}
		if item.Properties.LastModified != nil {
			object.LastModified = item.Properties.LastModified.UTC()
		}
		if item.Properties.ETag != nil && len(string(*item.Properties.ETag)) <= 512 {
			object.ETag = string(*item.Properties.ETag)
		}
		page.Objects = append(page.Objects, object)
	}
	if response.NextMarker != nil && *response.NextMarker != "" {
		encoded, err := s.cursor.Encode(request.Prefix, *response.NextMarker)
		if err != nil {
			return store.Page{}, &Error{fault.SafetyLimit}
		}
		page.NextCursor = encoded
	}
	return page, nil
}
func (s *Store) Open(ctx context.Context, request store.OpenRequest) (io.ReadCloser, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Key, "/") {
		return nil, store.ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	response, err := s.client.DownloadStream(ctx, s.container, s.prefix+request.Key, nil)
	if err != nil {
		cancel()
		return nil, safeError(ctx, err)
	}
	return &body{Reader: io.LimitReader(response.Body, request.MaxBytes), body: response.Body, cancel: cancel}, nil
}
func (s *Store) Stat(ctx context.Context, request store.StatRequest) (store.Object, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Key, "/") {
		return store.Object{}, store.ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	response, err := s.client.ServiceClient().NewContainerClient(s.container).NewBlobClient(s.prefix+request.Key).GetProperties(ctx, nil)
	if err != nil {
		return store.Object{}, safeError(ctx, err)
	}
	if response.ContentLength == nil || *response.ContentLength < 0 {
		return store.Object{}, &Error{fault.Unavailable}
	}
	result := store.Object{Key: request.Key, Size: *response.ContentLength}
	if response.LastModified != nil {
		result.LastModified = response.LastModified.UTC()
	}
	if response.ETag != nil && len(string(*response.ETag)) <= 512 {
		result.ETag = string(*response.ETag)
	}
	return result, nil
}
func rooted(prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return prefix
}
func (s *Store) relative(full, requested string) (string, bool) {
	if !strings.HasPrefix(full, s.prefix) {
		return "", false
	}
	key := strings.TrimPrefix(full, s.prefix)
	return key, strings.HasPrefix(key, requested)
}
func ptr[T any](value T) *T { return &value }

type body struct {
	io.Reader
	body   io.ReadCloser
	cancel context.CancelFunc
}

func (b *body) Close() error { b.cancel(); return b.body.Close() }

type Error struct{ kind fault.Category }

func (e *Error) Error() string            { return "azure request failed: " + string(e.kind) }
func (e *Error) Category() fault.Category { return e.kind }
func safeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		switch responseError.StatusCode {
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
