package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	_ "github.com/go-sql-driver/mysql"
)

const defaultPort = "3306"

// NormalizeDSN converts URI-style DSNs into go-sql-driver/mysql DSN format.
func NormalizeDSN(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("dsn is empty")
	}

	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" {
		return uriToDriverDSN(u)
	}

	if _, err := mysqlDriver.ParseDSN(raw); err == nil {
		return raw, nil
	}

	return "", fmt.Errorf("unsupported DSN format")
}

// RedactDSN masks password information from DSN values for logs/reports.
func RedactDSN(raw string) string {
	if raw == "" {
		return raw
	}

	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" {
		if u.User != nil {
			if _, ok := u.User.Password(); ok {
				u.User = url.UserPassword(u.User.Username(), "***")
			}
		}
		return u.String()
	}

	cfg, err := mysqlDriver.ParseDSN(raw)
	if err != nil {
		return raw
	}
	if cfg.Passwd != "" {
		cfg.Passwd = "***"
	}
	return cfg.FormatDSN()
}

// OpenAndPing opens a MySQL connection and verifies connectivity.
func OpenAndPing(ctx context.Context, rawDSN string) (*sql.DB, error) {
	normalized, err := NormalizeDSN(rawDSN)
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
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return db, nil
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
