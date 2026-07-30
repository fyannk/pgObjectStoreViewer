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

package config

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	defaultListenAddr             = ":3000"
	defaultTrustedUserHeader      = "X-Forwarded-User"
	defaultCatalogRefreshInterval = 5 * time.Minute
	defaultStoreRequestTimeout    = 10 * time.Second
	defaultScanConcurrency        = 4
	defaultMaxObjectsPerScan      = 1_000_000
	defaultWALPageSize            = 200
	maxSecretFileBytes            = 64 * 1024
	maxConfigFileBytes            = 1024 * 1024
	maxConfiguredScopes           = 1_000
)

// RuntimeMode selects one explicit process surface.
type RuntimeMode string

const (
	RuntimeStandalone       RuntimeMode = ""
	RuntimePGConsoleSidecar RuntimeMode = "pgconsole-sidecar"
)

// RepositoryFormat identifies the only repository grammar allowed in a scope.
type RepositoryFormat string

const (
	FormatBarmanCloud RepositoryFormat = "barman-cloud"
	FormatPGBackRest  RepositoryFormat = "pgbackrest"
)

// Provider identifies the object-store transport independently of format.
type Provider string

const (
	ProviderS3    Provider = "s3"
	ProviderAzure Provider = "azure"
	ProviderGCS   Provider = "gcs"
)

// CredentialMode states where an eventual provider adapter obtains credentials.
type CredentialMode string

const (
	CredentialAmbient        CredentialMode = "ambient"
	CredentialStaticFiles    CredentialMode = "static-files"
	CredentialAWSWebIdentity CredentialMode = "aws-web-identity"
	CredentialConnectionFile CredentialMode = "connection-string-file"
	CredentialAccountKeyFile CredentialMode = "account-key-file"
	// #nosec G101 -- these are credential *mode* names, not credentials.
	CredentialSASTokenFile CredentialMode = "sas-token-file"
	CredentialJSONFile     CredentialMode = "json-file"
)

// Secret deliberately has no exported representation and always formats redacted.
type Secret struct {
	value []byte
}

// Bytes returns a copy for a provider adapter. Callers must not log the result.
func (s Secret) Bytes() []byte {
	return bytes.Clone(s.value)
}

func (s Secret) Empty() bool { return len(s.value) == 0 }

func (s Secret) String() string { return "[REDACTED]" }

func (s Secret) GoString() string { return "[REDACTED]" }

// Credentials contains only credentials selected for the configured provider.
type Credentials struct {
	Mode CredentialMode

	AWSAccessKeyID          Secret
	AWSSecretKey            Secret
	AWSSessionToken         Secret
	AWSRegion               string
	AWSRoleARN              string
	AWSWebIdentityTokenFile string

	AzureAccount          string
	AzureConnectionString Secret
	AzureAccountKey       Secret
	AzureSASToken         Secret

	GoogleCredentialsJSON Secret
}

// Config is the frozen single-repository environment contract for Slice 0.
type Config struct {
	RuntimeMode RuntimeMode

	RepositoryFormat RepositoryFormat
	Provider         Provider
	Destination      *url.URL
	Endpoint         *url.URL
	EndpointCABundle []byte

	BarmanServerNames []string
	PGBackRestStanzas []string
	PGBackRestCipher  Secret

	ListenAddr        string
	TrustedUserHeader string
	AllowDownload     bool
	EvidenceTokenFile string

	CNPGClusterNamespace string
	CNPGClusterUID       string
	CNPGClusterName      string

	CatalogRefreshInterval time.Duration
	StoreRequestTimeout    time.Duration
	ScanConcurrency        int
	MaxObjectsPerScan      int
	WALPageSize            int

	ExpectedRetentionPolicy   string
	ExpectedMinimumRedundancy *int
	Credentials               Credentials
}

// SecretValues returns copies solely for constructing a boundary redactor.
func (c Config) SecretValues() [][]byte {
	secrets := []Secret{
		c.PGBackRestCipher,
		c.Credentials.AWSAccessKeyID,
		c.Credentials.AWSSecretKey,
		c.Credentials.AWSSessionToken,
		c.Credentials.AzureConnectionString,
		c.Credentials.AzureAccountKey,
		c.Credentials.AzureSASToken,
		c.Credentials.GoogleCredentialsJSON,
	}
	values := make([][]byte, 0, len(secrets))
	for _, secret := range secrets {
		if !secret.Empty() {
			values = append(values, secret.Bytes())
		}
	}
	return values
}

// Error is safe to return or log: it never embeds an environment value or path.
type Error struct {
	Variable string
	Code     string
}

func (e *Error) Error() string {
	if e.Variable == "" {
		return "configuration invalid: " + e.Code
	}
	return fmt.Sprintf("configuration invalid: %s: %s", e.Variable, e.Code)
}

// Load reads only the documented variables through getenv. In tests and other
// injected environments, an empty value is treated as absent.
func Load(getenv func(string) string) (Config, error) {
	return load(getenv, func(key string) (string, bool) {
		value := getenv(key)
		return value, value != ""
	})
}

func load(getenv func(string) string, lookupEnv func(string) (string, bool)) (Config, error) {
	runtimeMode, err := parseRuntimeMode(getenv("RUNTIME_MODE"))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		RuntimeMode:            runtimeMode,
		CatalogRefreshInterval: defaultCatalogRefreshInterval,
		StoreRequestTimeout:    defaultStoreRequestTimeout,
		ScanConcurrency:        defaultScanConcurrency,
		MaxObjectsPerScan:      defaultMaxObjectsPerScan,
		WALPageSize:            defaultWALPageSize,
	}
	if runtimeMode == RuntimeStandalone {
		cfg.ListenAddr = defaultListenAddr
		cfg.TrustedUserHeader = defaultTrustedUserHeader
	}

	format, err := parseFormat(getenv("REPOSITORY_FORMAT"))
	if err != nil {
		return Config{}, err
	}
	cfg.RepositoryFormat = format

	provider, err := parseProvider(getenv("PROVIDER"))
	if err != nil {
		return Config{}, err
	}
	cfg.Provider = provider

	destination, err := parseDestination(provider, getenv("DESTINATION_PATH"))
	if err != nil {
		return Config{}, err
	}
	cfg.Destination = destination

	if raw := getenv("ENDPOINT_URL"); raw != "" {
		if provider != ProviderS3 {
			return Config{}, configError("ENDPOINT_URL", "supported only for s3")
		}
		cfg.Endpoint, err = parseEndpoint(raw)
		if err != nil {
			return Config{}, err
		}
	}

	if path := getenv("ENDPOINT_CA_FILE"); path != "" {
		if provider != ProviderS3 {
			return Config{}, configError("ENDPOINT_CA_FILE", "supported only for s3")
		}
		cfg.EndpointCABundle, err = readBoundedFile("ENDPOINT_CA_FILE", path, maxConfigFileBytes, false)
		if err != nil {
			return Config{}, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.EndpointCABundle) {
			return Config{}, configError("ENDPOINT_CA_FILE", "must contain at least one PEM certificate")
		}
	}

	if runtimeMode == RuntimePGConsoleSidecar {
		if _, present := lookupEnv("LISTEN_ADDR"); present {
			return Config{}, configError("LISTEN_ADDR", "does not apply to pgconsole-sidecar")
		}
		if _, present := lookupEnv("TRUSTED_USER_HEADER"); present {
			return Config{}, configError("TRUSTED_USER_HEADER", "does not apply to pgconsole-sidecar")
		}
	} else {
		if raw := getenv("LISTEN_ADDR"); raw != "" {
			cfg.ListenAddr = raw
		}
		if err := validateListenAddr(cfg.ListenAddr); err != nil {
			return Config{}, err
		}
		if raw, present := lookupEnv("TRUSTED_USER_HEADER"); present {
			cfg.TrustedUserHeader = raw
		}
		if cfg.TrustedUserHeader != "" && !validHeaderName(cfg.TrustedUserHeader) {
			return Config{}, configError("TRUSTED_USER_HEADER", "must be an HTTP field name or empty")
		}
	}

	if cfg.AllowDownload, err = parseBool(getenv("ALLOW_DOWNLOAD"), false, "ALLOW_DOWNLOAD"); err != nil {
		return Config{}, err
	}
	if runtimeMode == RuntimePGConsoleSidecar && cfg.AllowDownload {
		return Config{}, configError("ALLOW_DOWNLOAD", "must be false in pgconsole-sidecar")
	}
	if cfg.CatalogRefreshInterval, err = parseDuration(getenv("CATALOG_REFRESH_INTERVAL"), defaultCatalogRefreshInterval, 30*time.Second, 24*time.Hour, "CATALOG_REFRESH_INTERVAL"); err != nil {
		return Config{}, err
	}
	if cfg.StoreRequestTimeout, err = parseDuration(getenv("STORE_REQUEST_TIMEOUT"), defaultStoreRequestTimeout, time.Second, 5*time.Minute, "STORE_REQUEST_TIMEOUT"); err != nil {
		return Config{}, err
	}
	if cfg.ScanConcurrency, err = parseInt(getenv("SCAN_CONCURRENCY"), defaultScanConcurrency, 1, 64, "SCAN_CONCURRENCY"); err != nil {
		return Config{}, err
	}
	if cfg.MaxObjectsPerScan, err = parseInt(getenv("MAX_OBJECTS_PER_SCAN"), defaultMaxObjectsPerScan, 1_000, 10_000_000, "MAX_OBJECTS_PER_SCAN"); err != nil {
		return Config{}, err
	}
	if cfg.WALPageSize, err = parseInt(getenv("WAL_PAGE_SIZE"), defaultWALPageSize, 1, 1_000, "WAL_PAGE_SIZE"); err != nil {
		return Config{}, err
	}

	cfg.ExpectedRetentionPolicy = getenv("EXPECTED_RETENTION_POLICY")
	if len(cfg.ExpectedRetentionPolicy) > 256 || containsControl(cfg.ExpectedRetentionPolicy) {
		return Config{}, configError("EXPECTED_RETENTION_POLICY", "must be at most 256 characters without control characters")
	}
	if raw := getenv("EXPECTED_MINIMUM_REDUNDANCY"); raw != "" {
		value, parseErr := parseInt(raw, 0, 0, 100_000, "EXPECTED_MINIMUM_REDUNDANCY")
		if parseErr != nil {
			return Config{}, parseErr
		}
		cfg.ExpectedMinimumRedundancy = &value
	}

	if err := loadFormatConfig(&cfg, getenv); err != nil {
		return Config{}, err
	}
	if err := loadRuntimeConfig(&cfg, getenv); err != nil {
		return Config{}, err
	}
	if err := loadCredentials(&cfg, getenv); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadRuntimeConfig(cfg *Config, getenv func(string) string) error {
	if cfg.RuntimeMode == RuntimeStandalone {
		for _, variable := range []string{"EVIDENCE_TOKEN_FILE", "CNPG_CLUSTER_NAMESPACE", "CNPG_CLUSTER_UID", "CNPG_CLUSTER_NAME", "STORE_CREDENTIAL_MODE"} {
			if getenv(variable) != "" {
				return configError(variable, "requires pgconsole-sidecar")
			}
		}
		return nil
	}
	if cfg.RepositoryFormat != FormatBarmanCloud {
		return configError("REPOSITORY_FORMAT", "pgconsole-sidecar requires barman-cloud")
	}
	if cfg.Provider != ProviderS3 {
		return configError("PROVIDER", "pgconsole-sidecar requires s3")
	}
	if len(cfg.BarmanServerNames) != 1 {
		return configError("BARMAN_SERVER_NAMES", "pgconsole-sidecar requires exactly one server")
	}
	cfg.EvidenceTokenFile = getenv("EVIDENCE_TOKEN_FILE")
	if !validAbsolutePath(cfg.EvidenceTokenFile) {
		return configError("EVIDENCE_TOKEN_FILE", "must be an absolute clean path")
	}
	cfg.CNPGClusterNamespace = getenv("CNPG_CLUSTER_NAMESPACE")
	cfg.CNPGClusterUID = getenv("CNPG_CLUSTER_UID")
	cfg.CNPGClusterName = getenv("CNPG_CLUSTER_NAME")
	if invalidIdentity(cfg.CNPGClusterNamespace, 63, true) {
		return configError("CNPG_CLUSTER_NAMESPACE", "must be a bounded non-empty identifier")
	}
	if invalidIdentity(cfg.CNPGClusterUID, 128, true) {
		return configError("CNPG_CLUSTER_UID", "must be a bounded non-empty identifier")
	}
	if invalidIdentity(cfg.CNPGClusterName, 253, false) {
		return configError("CNPG_CLUSTER_NAME", "must be a bounded identifier or empty")
	}
	return nil
}

// LoadOS loads the current process environment and preserves explicitly empty
// values such as TRUSTED_USER_HEADER="".
func LoadOS() (Config, error) { return load(os.Getenv, os.LookupEnv) }

func loadFormatConfig(cfg *Config, getenv func(string) string) error {
	barman, err := parseNames("BARMAN_SERVER_NAMES", getenv("BARMAN_SERVER_NAMES"))
	if err != nil {
		return err
	}
	stanzas, err := parseNames("PGBACKREST_STANZAS", getenv("PGBACKREST_STANZAS"))
	if err != nil {
		return err
	}
	cipherPath := getenv("PGBACKREST_CIPHER_PASS_FILE")

	switch cfg.RepositoryFormat {
	case FormatBarmanCloud:
		if len(stanzas) > 0 {
			return configError("PGBACKREST_STANZAS", "does not apply to barman-cloud")
		}
		if cipherPath != "" {
			return configError("PGBACKREST_CIPHER_PASS_FILE", "does not apply to barman-cloud")
		}
		cfg.BarmanServerNames = barman
	case FormatPGBackRest:
		if len(barman) > 0 {
			return configError("BARMAN_SERVER_NAMES", "does not apply to pgbackrest")
		}
		cfg.PGBackRestStanzas = stanzas
		if cipherPath != "" {
			value, readErr := readBoundedFile("PGBACKREST_CIPHER_PASS_FILE", cipherPath, maxSecretFileBytes, true)
			if readErr != nil {
				return readErr
			}
			cfg.PGBackRestCipher = Secret{value: value}
		}
	}
	return nil
}

func loadCredentials(cfg *Config, getenv func(string) string) error {
	if getenv("AWS_ACCESS_KEY_ID") != "" || getenv("AWS_SECRET_ACCESS_KEY") != "" || getenv("AWS_SESSION_TOKEN") != "" {
		return configError("AWS_ACCESS_KEY_ID", "direct static credentials are prohibited; use credential files or workload identity")
	}

	providerFiles := map[Provider][]string{
		ProviderS3:    {"AWS_ACCESS_KEY_ID_FILE", "AWS_SECRET_ACCESS_KEY_FILE", "AWS_SESSION_TOKEN_FILE", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN"},
		ProviderAzure: {"AZURE_STORAGE_CONNECTION_STRING_FILE", "AZURE_STORAGE_ACCOUNT_KEY_FILE", "AZURE_STORAGE_SAS_TOKEN_FILE"},
		ProviderGCS:   {"GOOGLE_APPLICATION_CREDENTIALS"},
	}
	for provider, variables := range providerFiles {
		if provider == cfg.Provider {
			continue
		}
		for _, variable := range variables {
			if getenv(variable) != "" {
				return configError(variable, "does not apply to selected provider")
			}
		}
	}

	switch cfg.Provider {
	case ProviderS3:
		return loadS3Credentials(cfg, getenv)
	case ProviderAzure:
		return loadAzureCredentials(cfg, getenv)
	case ProviderGCS:
		return loadGCSCredentials(cfg, getenv)
	default:
		return configError("PROVIDER", "unsupported")
	}
}

func loadS3Credentials(cfg *Config, getenv func(string) string) error {
	accessPath := getenv("AWS_ACCESS_KEY_ID_FILE")
	secretPath := getenv("AWS_SECRET_ACCESS_KEY_FILE")
	tokenPath := getenv("AWS_SESSION_TOKEN_FILE")
	if (accessPath == "") != (secretPath == "") {
		return configError("AWS_ACCESS_KEY_ID_FILE", "access-key and secret-key files must be configured together")
	}
	if tokenPath != "" && accessPath == "" {
		return configError("AWS_SESSION_TOKEN_FILE", "requires access-key and secret-key files")
	}
	cfg.Credentials.AWSRegion = getenv("AWS_REGION")
	if len(cfg.Credentials.AWSRegion) > 128 || containsControl(cfg.Credentials.AWSRegion) {
		return configError("AWS_REGION", "must be at most 128 characters without control characters")
	}
	if cfg.RuntimeMode == RuntimePGConsoleSidecar {
		mode := CredentialMode(getenv("STORE_CREDENTIAL_MODE"))
		switch mode {
		case CredentialStaticFiles:
			if accessPath == "" || getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" || getenv("AWS_ROLE_ARN") != "" {
				return configError("STORE_CREDENTIAL_MODE", "static-files requires only the static credential files")
			}
		case CredentialAWSWebIdentity:
			if accessPath != "" || secretPath != "" || tokenPath != "" {
				return configError("STORE_CREDENTIAL_MODE", "aws-web-identity cannot use static credential files")
			}
			webIdentityPath := getenv("AWS_WEB_IDENTITY_TOKEN_FILE")
			roleARN := getenv("AWS_ROLE_ARN")
			if !validAbsolutePath(webIdentityPath) {
				return configError("AWS_WEB_IDENTITY_TOKEN_FILE", "must be an absolute clean path")
			}
			if !validAWSRoleARN(roleARN) {
				return configError("AWS_ROLE_ARN", "must be a bounded AWS role ARN")
			}
			if cfg.Credentials.AWSRegion == "" {
				return configError("AWS_REGION", "required with aws-web-identity")
			}
			cfg.Credentials.Mode = CredentialAWSWebIdentity
			cfg.Credentials.AWSWebIdentityTokenFile = webIdentityPath
			cfg.Credentials.AWSRoleARN = roleARN
			return nil
		default:
			return configError("STORE_CREDENTIAL_MODE", "must be static-files or aws-web-identity")
		}
	}
	if accessPath == "" {
		cfg.Credentials.Mode = CredentialAmbient
		return nil
	}
	access, err := readBoundedFile("AWS_ACCESS_KEY_ID_FILE", accessPath, maxSecretFileBytes, true)
	if err != nil {
		return err
	}
	secret, err := readBoundedFile("AWS_SECRET_ACCESS_KEY_FILE", secretPath, maxSecretFileBytes, true)
	if err != nil {
		return err
	}
	cfg.Credentials.Mode = CredentialStaticFiles
	cfg.Credentials.AWSAccessKeyID = Secret{value: access}
	cfg.Credentials.AWSSecretKey = Secret{value: secret}
	if tokenPath != "" {
		token, readErr := readBoundedFile("AWS_SESSION_TOKEN_FILE", tokenPath, maxSecretFileBytes, true)
		if readErr != nil {
			return readErr
		}
		cfg.Credentials.AWSSessionToken = Secret{value: token}
	}
	return nil
}

func parseRuntimeMode(raw string) (RuntimeMode, error) {
	switch RuntimeMode(raw) {
	case RuntimeStandalone, RuntimePGConsoleSidecar:
		return RuntimeMode(raw), nil
	default:
		return "", configError("RUNTIME_MODE", "must be pgconsole-sidecar or empty")
	}
}

func loadAzureCredentials(cfg *Config, getenv func(string) string) error {
	connectionPath := getenv("AZURE_STORAGE_CONNECTION_STRING_FILE")
	account := getenv("AZURE_STORAGE_ACCOUNT")
	keyPath := getenv("AZURE_STORAGE_ACCOUNT_KEY_FILE")
	sasPath := getenv("AZURE_STORAGE_SAS_TOKEN_FILE")
	if len(account) > 256 || containsControl(account) {
		return configError("AZURE_STORAGE_ACCOUNT", "must be at most 256 characters without control characters")
	}
	if connectionPath != "" {
		if account != "" || keyPath != "" || sasPath != "" {
			return configError("AZURE_STORAGE_CONNECTION_STRING_FILE", "cannot be combined with account credentials")
		}
		value, err := readBoundedFile("AZURE_STORAGE_CONNECTION_STRING_FILE", connectionPath, maxSecretFileBytes, true)
		if err != nil {
			return err
		}
		cfg.Credentials.Mode = CredentialConnectionFile
		cfg.Credentials.AzureConnectionString = Secret{value: value}
		return nil
	}
	if keyPath != "" && sasPath != "" {
		return configError("AZURE_STORAGE_ACCOUNT_KEY_FILE", "account-key and SAS-token files are mutually exclusive")
	}
	if (keyPath != "" || sasPath != "") && account == "" {
		return configError("AZURE_STORAGE_ACCOUNT", "required with account-key or SAS-token file")
	}
	cfg.Credentials.AzureAccount = account
	if keyPath == "" && sasPath == "" {
		cfg.Credentials.Mode = CredentialAmbient
		return nil
	}
	if keyPath != "" {
		value, err := readBoundedFile("AZURE_STORAGE_ACCOUNT_KEY_FILE", keyPath, maxSecretFileBytes, true)
		if err != nil {
			return err
		}
		cfg.Credentials.Mode = CredentialAccountKeyFile
		cfg.Credentials.AzureAccountKey = Secret{value: value}
		return nil
	}
	value, err := readBoundedFile("AZURE_STORAGE_SAS_TOKEN_FILE", sasPath, maxSecretFileBytes, true)
	if err != nil {
		return err
	}
	cfg.Credentials.Mode = CredentialSASTokenFile
	cfg.Credentials.AzureSASToken = Secret{value: value}
	return nil
}

func loadGCSCredentials(cfg *Config, getenv func(string) string) error {
	path := getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if path == "" {
		cfg.Credentials.Mode = CredentialAmbient
		return nil
	}
	value, err := readBoundedFile("GOOGLE_APPLICATION_CREDENTIALS", path, maxConfigFileBytes, true)
	if err != nil {
		return err
	}
	if !json.Valid(value) {
		return configError("GOOGLE_APPLICATION_CREDENTIALS", "must contain valid JSON")
	}
	cfg.Credentials.Mode = CredentialJSONFile
	cfg.Credentials.GoogleCredentialsJSON = Secret{value: value}
	return nil
}

func parseFormat(raw string) (RepositoryFormat, error) {
	switch RepositoryFormat(raw) {
	case FormatBarmanCloud, FormatPGBackRest:
		return RepositoryFormat(raw), nil
	default:
		return "", configError("REPOSITORY_FORMAT", "must be barman-cloud or pgbackrest")
	}
}

func parseProvider(raw string) (Provider, error) {
	switch Provider(raw) {
	case ProviderS3, ProviderAzure, ProviderGCS:
		return Provider(raw), nil
	default:
		return "", configError("PROVIDER", "must be s3, azure, or gcs")
	}
}

func parseDestination(provider Provider, raw string) (*url.URL, error) {
	if raw == "" {
		return nil, configError("DESTINATION_PATH", "required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, configError("DESTINATION_PATH", "must be a credential-free object-store URI")
	}
	expectedScheme := map[Provider]string{ProviderS3: "s3", ProviderAzure: "azure", ProviderGCS: "gs"}[provider]
	if parsed.Scheme != expectedScheme {
		return nil, configError("DESTINATION_PATH", "scheme does not match PROVIDER")
	}
	if strings.Contains(parsed.EscapedPath(), "//") || containsControl(raw) {
		return nil, configError("DESTINATION_PATH", "contains an invalid prefix")
	}
	return parsed, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || containsControl(raw) ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, configError("ENDPOINT_URL", "must be a credential-free http or https origin")
	}
	return parsed, nil
}

func validAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func invalidIdentity(value string, maximum int, required bool) bool {
	return required && value == "" || len(value) > maximum || containsControl(value)
}

func validAWSRoleARN(value string) bool {
	if invalidIdentity(value, 2048, true) {
		return false
	}
	parts := strings.SplitN(value, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] == "" || parts[2] != "iam" || parts[3] != "" || len(parts[4]) != 12 || !strings.HasPrefix(parts[5], "role/") || len(parts[5]) == len("role/") {
		return false
	}
	for _, character := range parts[4] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validateListenAddr(raw string) error {
	_, port, err := net.SplitHostPort(raw)
	if err != nil || port == "" {
		return configError("LISTEN_ADDR", "must be host:port")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return configError("LISTEN_ADDR", "port must be between 1 and 65535")
	}
	return nil
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}

func parseNames(variable, raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		name := strings.TrimSpace(item)
		if name == "" || len(name) > 128 || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || containsControl(name) {
			return nil, configError(variable, "contains an invalid scope name")
		}
		seen[name] = struct{}{}
		if len(seen) > maxConfiguredScopes {
			return nil, configError(variable, "contains too many scope names")
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func parseBool(raw string, fallback bool, variable string) (bool, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, configError(variable, "must be true or false")
	}
	return value, nil
}

func parseDuration(raw string, fallback, minimum, maximum time.Duration, variable string) (time.Duration, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, configError(variable, fmt.Sprintf("must be between %s and %s", minimum, maximum))
	}
	return value, nil
}

func parseInt(raw string, fallback, minimum, maximum int, variable string) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, configError(variable, fmt.Sprintf("must be between %d and %d", minimum, maximum))
	}
	return value, nil
}

func readBoundedFile(variable, path string, maximum int64, trimNewline bool) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, configError(variable, "file cannot be opened")
	}
	defer func() { _ = file.Close() }()
	value, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, configError(variable, "file cannot be read")
	}
	if int64(len(value)) > maximum {
		return nil, configError(variable, "file exceeds the size limit")
	}
	if trimNewline {
		value = bytes.TrimSuffix(value, []byte("\n"))
		value = bytes.TrimSuffix(value, []byte("\r"))
	}
	if len(value) == 0 {
		return nil, configError(variable, "file is empty")
	}
	return value, nil
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func configError(variable, code string) error {
	return &Error{Variable: variable, Code: code}
}

// IsError reports whether err is a redacted configuration error.
func IsError(err error) bool {
	var target *Error
	return errors.As(err, &target)
}
