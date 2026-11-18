// Package config loads and validates the service's environment.
//
// Every value is validated before the process starts serving: a missing or
// malformed value is a startup failure that names the offending variable, not
// a request-time surprise.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment variable names. They are exported so documentation and tests
// refer to one spelling.
const (
	EnvJMAPURL         = "MAILFLOW_JMAP_URL"
	EnvJMAPHostname    = "MAILFLOW_JMAP_HOSTNAME"
	EnvCAFile          = "MAILFLOW_CA_FILE"
	EnvListenAddr      = "MAILFLOW_LISTEN_ADDR"
	EnvRequestTimeout  = "MAILFLOW_REQUEST_TIMEOUT"
	EnvPageSizeDefault = "MAILFLOW_PAGE_SIZE_DEFAULT"
	EnvPageSizeMax     = "MAILFLOW_PAGE_SIZE_MAX"
	EnvBodyMaxBytes    = "MAILFLOW_BODY_MAX_BYTES"

	EnvDBHost            = "MAILFLOW_DB_HOST"
	EnvDBPort            = "MAILFLOW_DB_PORT"
	EnvDBName            = "MAILFLOW_DB_NAME"
	EnvDBUser            = "MAILFLOW_DB_USER"
	EnvDBPassword        = "MAILFLOW_DB_PASSWORD"
	EnvIdempotencyTTL    = "MAILFLOW_IDEMPOTENCY_TTL"
	EnvIdempotencyLease  = "MAILFLOW_IDEMPOTENCY_LEASE"
	EnvSendMaxRecipients = "MAILFLOW_SEND_MAX_RECIPIENTS"
	EnvSendMaxBytes      = "MAILFLOW_SEND_MAX_BYTES"
	EnvOTLPEndpoint      = "MAILFLOW_OTEL_EXPORTER_OTLP_ENDPOINT"
)

// Defaults for the optional values.
const (
	DefaultListenAddr      = "127.0.0.1:8080"
	DefaultRequestTimeout  = 10 * time.Second
	DefaultPageSizeDefault = 25
	DefaultPageSizeMax     = 100
	DefaultBodyMaxBytes    = 256 * 1024

	DefaultDBHost            = "db"
	DefaultDBPort            = 5432
	DefaultDBName            = "mailflow"
	DefaultDBUser            = "mailflow"
	DefaultIdempotencyTTL    = 24 * time.Hour
	DefaultIdempotencyLease  = 2 * time.Minute
	DefaultSendMaxRecipients = 50
	DefaultSendMaxBytes      = 256 * 1024
)

// maxSendBytes bounds the configured send body. It keeps one request well
// inside the mail server's advertised request size and a small process memory
// budget.
const maxSendBytes = 8 << 20

// Config is the validated service configuration.
type Config struct {
	// JMAPURL is the mail server's JMAP endpoint, always ending in "/".
	JMAPURL *url.URL
	// JMAPHostname is the name the mail server's certificate must cover. It
	// is sent as the TLS server name even when JMAPURL names a container or
	// an address.
	JMAPHostname string
	// CAFile is the PEM bundle used to verify the mail server.
	CAFile string

	ListenAddr      string
	RequestTimeout  time.Duration
	PageSizeDefault int
	PageSizeMax     int
	BodyMaxBytes    int64

	// Database connection. The password is never logged.
	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string

	// IdempotencyTTL is how long a send key resolves to its earlier result.
	IdempotencyTTL time.Duration
	// IdempotencyLease bounds how long an unfinished claim blocks its key.
	IdempotencyLease time.Duration

	SendMaxRecipients int
	SendMaxBytes      int64

	// OTLPEndpoint is optional. When set, traces are exported over HTTP; an
	// unavailable collector never fails a request.
	OTLPEndpoint string
}

// Load reads configuration through getenv. It returns every problem it finds,
// joined, so a fresh deployment reports all missing values at once.
func Load(getenv func(string) string) (Config, error) {
	var (
		cfg  Config
		errs []error
	)

	jmapURL, err := parseURL(getenv(EnvJMAPURL))
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvJMAPURL, err))
	}
	cfg.JMAPURL = jmapURL

	cfg.JMAPHostname = strings.TrimSpace(getenv(EnvJMAPHostname))
	if cfg.JMAPHostname == "" {
		errs = append(errs, fmt.Errorf("%s: required", EnvJMAPHostname))
	} else if err := validateHostname(cfg.JMAPHostname); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvJMAPHostname, err))
	}

	cfg.CAFile = strings.TrimSpace(getenv(EnvCAFile))
	if cfg.CAFile == "" {
		errs = append(errs, fmt.Errorf("%s: required", EnvCAFile))
	} else if info, statErr := os.Stat(cfg.CAFile); statErr != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvCAFile, statErr))
	} else if !info.Mode().IsRegular() {
		errs = append(errs, fmt.Errorf("%s: %s is not a regular file", EnvCAFile, cfg.CAFile))
	}

	cfg.ListenAddr = strings.TrimSpace(getenv(EnvListenAddr))
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	if err := validateListenAddr(cfg.ListenAddr); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvListenAddr, err))
	}

	cfg.RequestTimeout, err = parseDuration(getenv(EnvRequestTimeout), DefaultRequestTimeout)
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvRequestTimeout, err))
	}

	cfg.PageSizeDefault, err = parsePageSize(getenv(EnvPageSizeDefault), DefaultPageSizeDefault, EnvPageSizeDefault)
	if err != nil {
		errs = append(errs, err)
	}

	cfg.PageSizeMax, err = parsePageSize(getenv(EnvPageSizeMax), DefaultPageSizeMax, EnvPageSizeMax)
	if err != nil {
		errs = append(errs, err)
	}

	if cfg.PageSizeDefault > cfg.PageSizeMax {
		errs = append(errs, fmt.Errorf("%s: %d exceeds %s (%d)",
			EnvPageSizeDefault, cfg.PageSizeDefault, EnvPageSizeMax, cfg.PageSizeMax))
	}

	cfg.BodyMaxBytes, err = parseBodyMax(getenv(EnvBodyMaxBytes))
	if err != nil {
		errs = append(errs, err)
	}

	cfg.DBHost = strings.TrimSpace(getenv(EnvDBHost))
	if cfg.DBHost == "" {
		cfg.DBHost = DefaultDBHost
	}
	if err := validateDBHost(cfg.DBHost); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvDBHost, err))
	}

	cfg.DBPort, err = parsePort(getenv(EnvDBPort), DefaultDBPort, EnvDBPort)
	if err != nil {
		errs = append(errs, err)
	}

	cfg.DBName = strings.TrimSpace(getenv(EnvDBName))
	if cfg.DBName == "" {
		cfg.DBName = DefaultDBName
	}
	if err := validateDBIdentifier(EnvDBName, cfg.DBName); err != nil {
		errs = append(errs, err)
	}

	cfg.DBUser = strings.TrimSpace(getenv(EnvDBUser))
	if cfg.DBUser == "" {
		cfg.DBUser = DefaultDBUser
	}
	if err := validateDBIdentifier(EnvDBUser, cfg.DBUser); err != nil {
		errs = append(errs, err)
	}

	// The password is used verbatim: trimming it would silently change it.
	// Only its absence is reported, never its value.
	cfg.DBPassword = getenv(EnvDBPassword)
	if cfg.DBPassword == "" {
		errs = append(errs, fmt.Errorf("%s: required", EnvDBPassword))
	}

	cfg.IdempotencyTTL, err = parseDuration(getenv(EnvIdempotencyTTL), DefaultIdempotencyTTL)
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvIdempotencyTTL, err))
	}

	cfg.IdempotencyLease, err = parseDuration(getenv(EnvIdempotencyLease), DefaultIdempotencyLease)
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvIdempotencyLease, err))
	}

	if cfg.IdempotencyLease > 0 && cfg.IdempotencyLease <= cfg.RequestTimeout {
		errs = append(errs, fmt.Errorf("%s: %s must exceed %s (%s)",
			EnvIdempotencyLease, cfg.IdempotencyLease, EnvRequestTimeout, cfg.RequestTimeout))
	}
	if cfg.IdempotencyTTL > 0 && cfg.IdempotencyLease > 0 && cfg.IdempotencyTTL <= cfg.IdempotencyLease {
		errs = append(errs, fmt.Errorf("%s: %s must exceed %s (%s)",
			EnvIdempotencyTTL, cfg.IdempotencyTTL, EnvIdempotencyLease, cfg.IdempotencyLease))
	}

	cfg.SendMaxRecipients, err = parseBoundedInt(getenv(EnvSendMaxRecipients),
		DefaultSendMaxRecipients, EnvSendMaxRecipients, 1000)
	if err != nil {
		errs = append(errs, err)
	}

	cfg.SendMaxBytes, err = parseBoundedInt64(getenv(EnvSendMaxBytes),
		DefaultSendMaxBytes, EnvSendMaxBytes, maxSendBytes)
	if err != nil {
		errs = append(errs, err)
	}

	cfg.OTLPEndpoint = strings.TrimSpace(getenv(EnvOTLPEndpoint))
	if cfg.OTLPEndpoint != "" {
		parsed, err := url.Parse(cfg.OTLPEndpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			errs = append(errs, fmt.Errorf("%s: must be an http or https endpoint", EnvOTLPEndpoint))
		}
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func parseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("not a URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("must use https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	if u.User != nil {
		// Credentials in configuration would be a credential store; callers
		// supply their own.
		return nil, errors.New("must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("must not contain a query or fragment")
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.RawPath = ""
	return u, nil
}

func validateHostname(name string) error {
	if strings.Contains(name, "://") || strings.ContainsAny(name, "/?#") {
		return fmt.Errorf("%q must be a bare hostname", name)
	}
	if _, _, err := net.SplitHostPort(name); err == nil {
		return fmt.Errorf("%q must not include a port", name)
	}
	return nil
}

func validateListenAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must be host:port: %w", err)
	}
	if host != "" {
		if ip := net.ParseIP(host); ip == nil {
			// A name is allowed, but must not be something odd such as a URL.
			if strings.ContainsAny(host, " /?#") {
				return fmt.Errorf("invalid host %q", host)
			}
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	return nil
}

func parseDuration(raw string, def time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("not a duration: %w", err)
	}
	if d <= 0 {
		return 0, errors.New("must be positive")
	}
	return d, nil
}

func parsePageSize(raw string, def int, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: not an integer: %w", name, err)
	}
	if n < 1 || n > 1000 {
		return 0, fmt.Errorf("%s: must be between 1 and 1000, got %d", name, n)
	}
	return n, nil
}

func parseBodyMax(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultBodyMaxBytes, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: not an integer: %w", EnvBodyMaxBytes, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s: must be positive, got %d", EnvBodyMaxBytes, n)
	}
	return n, nil
}

// validateDBHost accepts a container name or an address, never a URL and never
// something carrying a port: the port is its own validated variable.
func validateDBHost(host string) error {
	if strings.ContainsAny(host, " /?#:") {
		return fmt.Errorf("%q must be a hostname without a scheme or port", host)
	}
	return nil
}

// validateDBIdentifier rejects an empty or whitespace-bearing database name or
// user. These are configuration, not secrets, so naming them is safe.
func validateDBIdentifier(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s: required", name)
	}
	if strings.ContainsAny(value, " /\\") {
		return fmt.Errorf("%s: must not contain whitespace or a path separator", name)
	}
	return nil
}

func parsePort(raw string, def int, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: not an integer: %w", name, err)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s: must be 1-65535, got %d", name, n)
	}
	return n, nil
}

func parseBoundedInt(raw string, def int, name string, max int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: not an integer: %w", name, err)
	}
	if n < 1 || n > max {
		return 0, fmt.Errorf("%s: must be between 1 and %d, got %d", name, max, n)
	}
	return n, nil
}

func parseBoundedInt64(raw string, def int64, name string, max int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: not an integer: %w", name, err)
	}
	if n < 1 || n > max {
		return 0, fmt.Errorf("%s: must be between 1 and %d, got %d", name, max, n)
	}
	return n, nil
}
