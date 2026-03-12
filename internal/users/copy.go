package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/esthergb/dbmigrate/internal/dblog"
)

var systemUsers = map[string]struct{}{
	"root":             {},
	"mysql.sys":        {},
	"mysql.session":    {},
	"mysql.infoschema": {},
	"mariadb.sys":      {},
	"PUBLIC":           {},
}

// Scope controls which users are included.
type Scope string

const (
	ScopeBusiness Scope = "business"
	ScopeAll      Scope = "all"
)

// CopyOptions controls user/grant migration behavior.
type CopyOptions struct {
	Scope      Scope
	DryRun     bool
	SkipLocked bool
	Log        *dblog.Logger
}

// UserSummary reports how many users and grants were applied.
type UserSummary struct {
	UsersFound   int
	UsersSkipped int
	UsersCopied  int
	GrantsCopied int
	DryRun       bool
}

// UserEntry holds the extracted DDL for one user account.
type UserEntry struct {
	User      string
	Host      string
	CreateSQL string
	Grants    []string
}

var queryUsersFn = queryUsers
var fetchCreateUserFn = fetchCreateUser
var fetchGrantsFn = fetchGrants

// CopyUsers extracts users from source and applies them to destination.
func CopyUsers(ctx context.Context, source *sql.DB, dest *sql.DB, opts CopyOptions) (UserSummary, error) {
	if source == nil || dest == nil {
		return UserSummary{}, errors.New("source and destination connections are required")
	}
	if opts.Scope == "" {
		opts.Scope = ScopeBusiness
	}

	users, err := queryUsersFn(ctx, source)
	if err != nil {
		return UserSummary{}, fmt.Errorf("list source users: %w", err)
	}

	summary := UserSummary{DryRun: opts.DryRun, UsersFound: len(users)}

	for _, u := range users {
		if shouldSkipUser(u.User, u.Host, opts.Scope) {
			summary.UsersSkipped++
			if opts.Log != nil {
				opts.Log.Debug("skipping user", "user", u.User, "host", u.Host)
			}
			continue
		}

		createSQL, err := fetchCreateUserFn(ctx, source, u.User, u.Host)
		if err != nil {
			if opts.SkipLocked && isLockedAccountError(err) {
				summary.UsersSkipped++
				continue
			}
			return summary, fmt.Errorf("fetch CREATE USER for %s@%s: %w", u.User, u.Host, err)
		}

		grants, err := fetchGrantsFn(ctx, source, u.User, u.Host)
		if err != nil {
			return summary, fmt.Errorf("fetch GRANTS for %s@%s: %w", u.User, u.Host, err)
		}

		entry := UserEntry{
			User:      u.User,
			Host:      u.Host,
			CreateSQL: createSQL,
			Grants:    grants,
		}

		if opts.Log != nil {
			opts.Log.Debug("copying user", "user", u.User, "host", u.Host, "grants", len(grants))
		}

		if !opts.DryRun {
			if err := applyUser(ctx, dest, entry); err != nil {
				return summary, fmt.Errorf("apply user %s@%s: %w", u.User, u.Host, err)
			}
		}

		summary.UsersCopied++
		summary.GrantsCopied += len(grants)
	}

	return summary, nil
}

func shouldSkipUser(user, host string, scope Scope) bool {
	if scope == ScopeAll {
		return false
	}
	if _, ok := systemUsers[user]; ok {
		return true
	}
	if strings.HasSuffix(user, ".sys") || strings.HasSuffix(user, ".session") || strings.HasSuffix(user, ".infoschema") {
		return true
	}
	_ = host
	return false
}

type userRef struct {
	User string
	Host string
}

func queryUsers(ctx context.Context, db *sql.DB) ([]userRef, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT User, Host
		FROM mysql.user
		ORDER BY User, Host
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []userRef
	for rows.Next() {
		var u userRef
		if err := rows.Scan(&u.User, &u.Host); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func quoteUser(user, host string) string {
	return fmt.Sprintf("'%s'@'%s'", escapeSQLString(user), escapeSQLString(host))
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`)
}

func fetchCreateUser(ctx context.Context, db *sql.DB, user, host string) (string, error) {
	query := fmt.Sprintf("SHOW CREATE USER %s", quoteUser(user, host))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", sql.ErrNoRows
	}
	var createSQL string
	if err := rows.Scan(&createSQL); err != nil {
		return "", err
	}
	return sanitizeCreateUser(createSQL), nil
}

func fetchGrants(ctx context.Context, db *sql.DB, user, host string) ([]string, error) {
	query := fmt.Sprintf("SHOW GRANTS FOR %s", quoteUser(user, host))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var grants []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func applyUser(ctx context.Context, dest *sql.DB, entry UserEntry) error {
	conn, err := dest.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	sanitized := sanitizeCreateUser(entry.CreateSQL)
	if _, err := conn.ExecContext(ctx, sanitized); err != nil {
		if !isUserAlreadyExistsError(err) {
			return fmt.Errorf("create user: %w", err)
		}
	}

	sortedGrants := make([]string, len(entry.Grants))
	copy(sortedGrants, entry.Grants)
	sort.Strings(sortedGrants)

	for _, grant := range sortedGrants {
		if isUsageGrant(grant) {
			continue
		}
		if _, err := conn.ExecContext(ctx, grant); err != nil {
			return fmt.Errorf("apply grant: %w", err)
		}
	}

	if _, err := conn.ExecContext(ctx, "FLUSH PRIVILEGES"); err != nil {
		return fmt.Errorf("flush privileges: %w", err)
	}

	return nil
}

func sanitizeCreateUser(createSQL string) string {
	return strings.TrimSpace(createSQL)
}

func isUserAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "operation create user failed") ||
		strings.Contains(msg, "error 1396") ||
		strings.Contains(msg, "er_cannot_user")
}

func isLockedAccountError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "account is locked") ||
		strings.Contains(msg, "er_account_has_been_locked")
}

func isUsageGrant(grant string) bool {
	upper := strings.ToUpper(strings.TrimSpace(grant))
	return strings.HasPrefix(upper, "GRANT USAGE ON")
}
