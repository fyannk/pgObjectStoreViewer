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

// Package s3 implements the provider-neutral read store using only S3 list,
// get, and head operations. AWS SDK types never leave this package.
package s3

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/fyannk/objectstoreviewer/internal/fault"
	"github.com/fyannk/objectstoreviewer/internal/store"
)

const (
	cursorMACBytes    = sha256.Size
	maxRequestTimeout = 5 * time.Minute
)

type Options struct {
	Bucket               string
	Prefix               string
	Endpoint             *url.URL
	CABundle             []byte
	Region               string
	AccessKeyID          []byte
	SecretAccessKey      []byte
	SessionToken         []byte
	WebIdentityTokenFile string
	RoleARN              string
	RequestTimeout       time.Duration
}

type api interface {
	ListObjectsV2(context.Context, *awss3.ListObjectsV2Input, ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error)
	GetObject(context.Context, *awss3.GetObjectInput, ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	HeadObject(context.Context, *awss3.HeadObjectInput, ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error)
}

type Store struct {
	client         api
	bucket         string
	rootPrefix     string
	requestTimeout time.Duration
	cursorKey      []byte
}

func New(ctx context.Context, options Options) (*Store, error) {
	if invalidOptions(options) {
		return nil, &Error{kind: fault.InvalidConfig, operation: "configure"}
	}
	loadOptions := make([]func(*awsconfig.LoadOptions) error, 0, 3)
	region := options.Region
	if region == "" && options.Endpoint != nil {
		region = "us-east-1"
	}
	if region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(region))
	}
	if len(options.AccessKeyID) > 0 {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			string(options.AccessKeyID), string(options.SecretAccessKey), string(options.SessionToken),
		)))
	} else if options.WebIdentityTokenFile != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}))
	}
	if len(options.CABundle) > 0 {
		httpClient, err := httpClientWithCA(options.CABundle)
		if err != nil {
			return nil, &Error{kind: fault.InvalidConfig, operation: "configure"}
		}
		loadOptions = append(loadOptions, awsconfig.WithHTTPClient(httpClient))
	}
	sdkConfig, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, &Error{kind: fault.InvalidConfig, operation: "configure"}
	}
	if options.WebIdentityTokenFile != "" {
		provider := stscreds.NewWebIdentityRoleProvider(
			sts.NewFromConfig(sdkConfig), options.RoleARN,
			stscreds.IdentityTokenFile(options.WebIdentityTokenFile),
			func(providerOptions *stscreds.WebIdentityRoleOptions) {
				providerOptions.RoleSessionName = "objectstoreviewer"
			},
		)
		sdkConfig.Credentials = aws.NewCredentialsCache(provider)
	}
	client := awss3.NewFromConfig(sdkConfig, func(optionsValue *awss3.Options) {
		optionsValue.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		optionsValue.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		if options.Endpoint != nil {
			optionsValue.BaseEndpoint = aws.String(options.Endpoint.String())
			optionsValue.UsePathStyle = true
		}
	})
	cursorKey := make([]byte, 32)
	if _, err := rand.Read(cursorKey); err != nil {
		return nil, &Error{kind: fault.Unavailable, operation: "configure"}
	}
	return newWithAPI(client, options, cursorKey)
}

func newWithAPI(client api, options Options, cursorKey []byte) (*Store, error) {
	if client == nil || invalidOptions(options) || len(cursorKey) < 32 {
		return nil, &Error{kind: fault.InvalidConfig, operation: "configure"}
	}
	prefix := strings.Trim(options.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &Store{
		client: client, bucket: options.Bucket, rootPrefix: prefix,
		requestTimeout: options.RequestTimeout, cursorKey: append([]byte(nil), cursorKey...),
	}, nil
}

func invalidOptions(options Options) bool {
	if options.Bucket == "" || len(options.Bucket) > 255 || strings.IndexFunc(options.Bucket, unicode.IsControl) >= 0 ||
		options.RequestTimeout <= 0 || options.RequestTimeout > maxRequestTimeout || len(options.Prefix) > store.MaxKeyBytes ||
		len(options.Region) > 128 || strings.IndexFunc(options.Region, unicode.IsControl) >= 0 || len(options.CABundle) > 1024*1024 ||
		len(options.AccessKeyID) > 64*1024 || len(options.SecretAccessKey) > 64*1024 || len(options.SessionToken) > 64*1024 ||
		len(options.WebIdentityTokenFile) > 4096 || len(options.RoleARN) > 2048 || strings.IndexFunc(options.WebIdentityTokenFile, unicode.IsControl) >= 0 || strings.IndexFunc(options.RoleARN, unicode.IsControl) >= 0 ||
		(len(options.AccessKeyID) == 0) != (len(options.SecretAccessKey) == 0) || (len(options.SessionToken) > 0 && len(options.AccessKeyID) == 0) ||
		(options.WebIdentityTokenFile == "") != (options.RoleARN == "") || (len(options.AccessKeyID) > 0 && options.WebIdentityTokenFile != "") {
		return true
	}
	if options.Endpoint == nil {
		return false
	}
	endpoint := options.Endpoint
	return endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawPath != "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https")
}

func (s *Store) List(ctx context.Context, request store.ListRequest) (store.Page, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Prefix, "/") {
		return store.Page{}, store.ErrInvalidRequest
	}
	continuation, err := s.decodeCursor(request.Prefix, request.Cursor)
	if err != nil {
		return store.Page{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	input := &awss3.ListObjectsV2Input{
		// #nosec G115 -- ListRequest.Validate bounds Limit to 1..store.MaxPageObjects (1000).
		Bucket: aws.String(s.bucket), Prefix: aws.String(s.rootPrefix + request.Prefix), MaxKeys: aws.Int32(int32(request.Limit)),
	}
	if continuation != "" {
		input.ContinuationToken = aws.String(continuation)
	}
	output, err := s.client.ListObjectsV2(requestCtx, input)
	if err != nil {
		return store.Page{}, safeError(requestCtx, "list", err)
	}
	if output == nil || len(output.Contents) > request.Limit {
		return store.Page{}, &Error{kind: fault.Unavailable, operation: "list"}
	}
	page := store.Page{Objects: make([]store.Object, 0, len(output.Contents))}
	for _, object := range output.Contents {
		if object.Key == nil || object.Size == nil || *object.Size < 0 {
			return store.Page{}, &Error{kind: fault.Unavailable, operation: "list"}
		}
		key, ok := s.relativeKey(*object.Key, request.Prefix)
		if !ok || key == "" || len(key) > store.MaxKeyBytes {
			return store.Page{}, &Error{kind: fault.SafetyLimit, operation: "list"}
		}
		item := store.Object{Key: key, Size: *object.Size}
		if object.LastModified != nil {
			item.LastModified = object.LastModified.UTC()
		}
		if object.ETag != nil && len(*object.ETag) <= 512 {
			item.ETag = *object.ETag
		}
		page.Objects = append(page.Objects, item)
	}
	if aws.ToBool(output.IsTruncated) {
		if output.NextContinuationToken == nil || *output.NextContinuationToken == "" {
			return store.Page{}, &Error{kind: fault.Unavailable, operation: "list"}
		}
		page.NextCursor, err = s.encodeCursor(request.Prefix, *output.NextContinuationToken)
		if err != nil {
			return store.Page{}, err
		}
	}
	return page, nil
}

func (s *Store) Open(ctx context.Context, request store.OpenRequest) (io.ReadCloser, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Key, "/") {
		return nil, store.ErrInvalidRequest
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	output, err := s.client.GetObject(requestCtx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.rootPrefix + request.Key),
	})
	if err != nil {
		cancel()
		return nil, safeError(requestCtx, "open", err)
	}
	if output == nil || output.Body == nil {
		cancel()
		return nil, &Error{kind: fault.Unavailable, operation: "open"}
	}
	return &limitedBody{Reader: io.LimitReader(output.Body, request.MaxBytes), body: output.Body, cancel: cancel}, nil
}

func (s *Store) Stat(ctx context.Context, request store.StatRequest) (store.Object, error) {
	if err := request.Validate(); err != nil || strings.HasPrefix(request.Key, "/") {
		return store.Object{}, store.ErrInvalidRequest
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	output, err := s.client.HeadObject(requestCtx, &awss3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.rootPrefix + request.Key),
	})
	if err != nil {
		return store.Object{}, safeError(requestCtx, "stat", err)
	}
	if output == nil || output.ContentLength == nil || *output.ContentLength < 0 {
		return store.Object{}, &Error{kind: fault.Unavailable, operation: "stat"}
	}
	result := store.Object{Key: request.Key, Size: *output.ContentLength}
	if output.LastModified != nil {
		result.LastModified = output.LastModified.UTC()
	}
	if output.ETag != nil && len(*output.ETag) <= 512 {
		result.ETag = *output.ETag
	}
	return result, nil
}

func (s *Store) relativeKey(fullKey, requestedPrefix string) (string, bool) {
	if !strings.HasPrefix(fullKey, s.rootPrefix) {
		return "", false
	}
	relative := strings.TrimPrefix(fullKey, s.rootPrefix)
	return relative, strings.HasPrefix(relative, requestedPrefix)
}

func (s *Store) encodeCursor(prefix, token string) (string, error) {
	if len(prefix) > store.MaxKeyBytes || len(token) > store.MaxCursorBytes-cursorMACBytes-4 {
		return "", &Error{kind: fault.SafetyLimit, operation: "list"}
	}
	body := make([]byte, 4+len(prefix)+len(token))
	// #nosec G115 -- the guard above bounds len(prefix) to store.MaxKeyBytes (16 KiB).
	binary.BigEndian.PutUint32(body[:4], uint32(len(prefix)))
	copy(body[4:], prefix)
	copy(body[4+len(prefix):], token)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(body)
	encoded := base64.RawURLEncoding.EncodeToString(append(body, mac.Sum(nil)...))
	if len(encoded) > store.MaxCursorBytes {
		return "", &Error{kind: fault.SafetyLimit, operation: "list"}
	}
	return encoded, nil
}

func (s *Store) decodeCursor(prefix, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) < 4+cursorMACBytes {
		return "", store.ErrInvalidRequest
	}
	body, signature := decoded[:len(decoded)-cursorMACBytes], decoded[len(decoded)-cursorMACBytes:]
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", store.ErrInvalidRequest
	}
	prefixLength := int(binary.BigEndian.Uint32(body[:4]))
	if prefixLength > len(body)-4 || string(body[4:4+prefixLength]) != prefix {
		return "", store.ErrInvalidRequest
	}
	return string(body[4+prefixLength:]), nil
}

type limitedBody struct {
	io.Reader
	body   io.Closer
	cancel context.CancelFunc
}

func (b *limitedBody) Close() error {
	b.cancel()
	return b.body.Close()
}

type Error struct {
	kind      fault.Category
	operation string
}

func (e *Error) Error() string { return fmt.Sprintf("s3 %s failed: %s", e.operation, e.kind) }

func (e *Error) Category() fault.Category { return e.kind }

func safeError(ctx context.Context, operation string, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		return &Error{kind: categoryForCode(apiError.ErrorCode()), operation: operation}
	}
	return &Error{kind: fault.Unavailable, operation: operation}
}

func categoryForCode(code string) fault.Category {
	switch code {
	case "AccessDenied", "AllAccessDisabled":
		return fault.Authorization
	case "ExpiredToken", "InvalidAccessKeyId", "InvalidToken", "SignatureDoesNotMatch":
		return fault.Authentication
	case "NoSuchBucket", "NoSuchKey", "NotFound":
		return fault.NotFound
	case "SlowDown", "Throttling", "ThrottlingException", "TooManyRequestsException":
		return fault.Throttled
	default:
		return fault.Unavailable
	}
}

func httpClientWithCA(bundle []byte) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(bundle) {
		return nil, errors.New("invalid CA bundle")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	return &http.Client{Transport: transport}, nil
}

var _ store.Reader = (*Store)(nil)
