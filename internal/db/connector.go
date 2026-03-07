package db

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	_ "github.com/go-sql-driver/mysql"
)

const defaultPort = "3306"

// TLSOptions describes runtime TLS settings.
type TLSOptions struct {
	Mode     string
	CAFile   string
	CertFile string
	KeyFile  string
}

var (
	tlsRegistryCounter uint64
	tlsRegistryMu      sync.Mutex
)

// NormalizeDSN converts URI-style DSNs into go-sql-driver/mysql DSN format.
func NormalizeDSN(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("dsn is empty")
	}

	if _, err := mysqlDriver.ParseDSN(raw); err == nil {
		return raw, nil
	}

	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" {
		return uriToDriverDSN(u)
	}

	return "", fmt.Errorf("unsupported DSN format")
}

// RedactDSN masks password information from DSN values for logs/reports.
func RedactDSN(raw string) string {
	if raw == "" {
		return raw
	}

	cfg, err := mysqlDriver.ParseDSN(raw)
	if err == nil {
		if cfg.Passwd != "" {
			cfg.Passwd = "***"
		}
		return cfg.FormatDSN()
	}

	u, err := url.Parse(raw)
	if err == nil && (strings.EqualFold(u.Scheme, "mysql") || strings.EqualFold(u.Scheme, "mariadb")) {
		if u.User != nil {
			if _, ok := u.User.Password(); ok {
				u.User = url.UserPassword(u.User.Username(), "***")
			}
		}
		return u.String()
	}

	return raw
}

// OpenAndPing opens a MySQL connection and verifies connectivity.
func OpenAndPing(ctx context.Context, rawDSN string) (*sql.DB, error) {
	return OpenAndPingWithTLS(ctx, rawDSN, TLSOptions{})
}

// OpenAndPingWithTLS opens a MySQL connection and verifies connectivity using runtime TLS options.
func OpenAndPingWithTLS(ctx context.Context, rawDSN string, tlsOptions TLSOptions) (*sql.DB, error) {
	normalized, err := NormalizeDSNWithTLS(rawDSN, tlsOptions)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", normalized)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	deadlineCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		deadlineCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	if err := db.PingContext(deadlineCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return db, nil
}

// NormalizeDSNWithTLS converts URI-style DSNs into driver format and applies runtime TLS options.
func NormalizeDSNWithTLS(raw string, tlsOptions TLSOptions) (string, error) {
	cfg, err := BuildDriverConfig(raw, tlsOptions)
	if err != nil {
		return "", err
	}
	return cfg.FormatDSN(), nil
}

// BuildDriverConfig parses source DSN and applies runtime TLS options.
func BuildDriverConfig(raw string, tlsOptions TLSOptions) (*mysqlDriver.Config, error) {
	normalized, err := NormalizeDSN(raw)
	if err != nil {
		return nil, err
	}
	cfg, err := mysqlDriver.ParseDSN(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if err := applyRuntimeTLS(cfg, tlsOptions); err != nil {
		return nil, err
	}
	return cfg, nil
}

func uriToDriverDSN(u *url.URL) (string, error) {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "mysql" && scheme != "mariadb" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return "", errors.New("dsn host is empty")
	}

	port := u.Port()
	if port == "" {
		port = defaultPort
	}

	cfg := mysqlDriver.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, port)
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}

	if u.User != nil {
		cfg.User = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			cfg.Passwd = pass
		}
	}

	dbName := strings.TrimPrefix(u.Path, "/")
	cfg.DBName = dbName

	q := u.Query()
	if tlsMode := q.Get("tls"); tlsMode != "" {
		cfg.TLSConfig = tlsMode
		q.Del("tls")
	}

	for k, values := range q {
		if len(values) == 0 {
			continue
		}
		cfg.Params[k] = values[len(values)-1]
	}

	return cfg.FormatDSN(), nil
}

func applyRuntimeTLS(cfg *mysqlDriver.Config, tlsOptions TLSOptions) error {
	mode := strings.TrimSpace(strings.ToLower(tlsOptions.Mode))
	if mode == "" {
		return nil
	}
	hasCustomTLS := strings.TrimSpace(tlsOptions.CAFile) != "" ||
		strings.TrimSpace(tlsOptions.CertFile) != "" ||
		strings.TrimSpace(tlsOptions.KeyFile) != ""
	if mode == "disabled" {
		if hasCustomTLS {
			return errors.New("tls-mode=disabled cannot be used with --ca-file/--cert-file/--key-file")
		}
		cfg.TLSConfig = "false"
		cfg.TLS = nil
		cfg.AllowFallbackToPlaintext = false
		return nil
	}
	if mode != "preferred" && mode != "required" {
		return fmt.Errorf("invalid tls mode %q", tlsOptions.Mode)
	}

	if !hasCustomTLS {
		if mode == "required" {
			cfg.TLSConfig = "true"
			cfg.AllowFallbackToPlaintext = false
		} else {
			cfg.TLSConfig = "preferred"
			cfg.AllowFallbackToPlaintext = true
		}
		return nil
	}

	customTLS, err := buildCustomTLSConfig(cfg, tlsOptions, mode)
	if err != nil {
		return err
	}

	name, err := registerTLSConfig(customTLS)
	if err != nil {
		return err
	}
	cfg.TLS = customTLS
	cfg.TLSConfig = name
	cfg.AllowFallbackToPlaintext = mode == "preferred"
	return nil
}

func buildCustomTLSConfig(cfg *mysqlDriver.Config, tlsOptions TLSOptions, mode string) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err == nil && strings.TrimSpace(host) != "" {
		tlsCfg.ServerName = host
	}

	if strings.TrimSpace(tlsOptions.CAFile) != "" {
		caBytes, err := os.ReadFile(filepath.Clean(tlsOptions.CAFile))
		if err != nil {
			return nil, fmt.Errorf("read ca-file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caBytes) {
			return nil, errors.New("parse ca-file: no certificates found")
		}
		tlsCfg.RootCAs = roots
	}

	certFile := strings.TrimSpace(tlsOptions.CertFile)
	keyFile := strings.TrimSpace(tlsOptions.KeyFile)
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("both --cert-file and --key-file are required for client TLS auth")
		}
		cert, err := tls.LoadX509KeyPair(filepath.Clean(certFile), filepath.Clean(keyFile))
		if err != nil {
			return nil, fmt.Errorf("load client cert/key pair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if mode == "required" {
		tlsCfg.InsecureSkipVerify = false
	}
	return tlsCfg, nil
}

func registerTLSConfig(cfg *tls.Config) (string, error) {
	if cfg == nil {
		return "", errors.New("tls config is nil")
	}
	name := fmt.Sprintf("dbmigrate_tls_%d", atomic.AddUint64(&tlsRegistryCounter, 1))
	tlsRegistryMu.Lock()
	defer tlsRegistryMu.Unlock()
	if err := mysqlDriver.RegisterTLSConfig(name, cfg); err != nil {
		return "", fmt.Errorf("register mysql tls config: %w", err)
	}
	return name, nil
}
