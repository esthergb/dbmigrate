package users

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestShouldSkipUserBusinessScope(t *testing.T) {
	cases := []struct {
		user string
		host string
		skip bool
	}{
		{"root", "localhost", true},
		{"mysql.sys", "localhost", true},
		{"mysql.session", "localhost", true},
		{"mysql.infoschema", "localhost", true},
		{"mariadb.sys", "localhost", true},
		{"app_user", "localhost", false},
		{"reporting", "%", false},
		{"deploy", "10.0.0.%", false},
	}
	for _, tc := range cases {
		got := shouldSkipUser(tc.user, tc.host, ScopeBusiness)
		if got != tc.skip {
			t.Errorf("shouldSkipUser(%q, %q, business) = %v, want %v", tc.user, tc.host, got, tc.skip)
		}
	}
}

func TestShouldSkipUserAllScope(t *testing.T) {
	if shouldSkipUser("root", "localhost", ScopeAll) {
		t.Error("expected root to NOT be skipped with scope=all")
	}
	if shouldSkipUser("mysql.sys", "%", ScopeAll) {
		t.Error("expected mysql.sys to NOT be skipped with scope=all")
	}
}

func TestIsUsageGrant(t *testing.T) {
	cases := []struct {
		grant string
		usage bool
	}{
		{"GRANT USAGE ON *.* TO 'app'@'localhost'", true},
		{"GRANT SELECT ON `db`.* TO 'app'@'localhost'", false},
		{"GRANT ALL PRIVILEGES ON *.* TO 'app'@'localhost'", false},
		{"grant usage on *.* to 'x'@'%'", true},
	}
	for _, tc := range cases {
		got := isUsageGrant(tc.grant)
		if got != tc.usage {
			t.Errorf("isUsageGrant(%q) = %v, want %v", tc.grant, got, tc.usage)
		}
	}
}

func TestIsUserAlreadyExistsError(t *testing.T) {
	if isUserAlreadyExistsError(nil) {
		t.Error("expected nil error to return false")
	}
	if !isUserAlreadyExistsError(fmt.Errorf("operation create user failed for 'x'@'localhost'")) {
		t.Error("expected create user failed error to be detected")
	}
	if !isUserAlreadyExistsError(fmt.Errorf("ERROR 1396 (HY000): Operation CREATE USER failed")) {
		t.Error("expected error 1396 to be detected")
	}
}

func TestSanitizeCreateUser(t *testing.T) {
	in := "  CREATE USER 'app'@'localhost' IDENTIFIED BY 'secret'  "
	out := sanitizeCreateUser(in)
	if out != "CREATE USER 'app'@'localhost' IDENTIFIED BY 'secret'" {
		t.Fatalf("unexpected sanitized output: %q", out)
	}
}

func TestCopyUsersRequiresConnections(t *testing.T) {
	_, err := CopyUsers(context.Background(), nil, nil, CopyOptions{})
	if err == nil {
		t.Fatal("expected error for nil connections")
	}
}

func TestCopyUsersDefaultsToBusinessScope(t *testing.T) {
	called := false
	queryUsersFn = func(_ context.Context, _ *sql.DB) ([]userRef, error) {
		called = true
		return []userRef{}, nil
	}
	defer func() { queryUsersFn = queryUsers }()

	source := &sql.DB{}
	dest := &sql.DB{}
	_, err := CopyUsers(context.Background(), source, dest, CopyOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected queryUsersFn to be called")
	}
}

func TestCopyUsersDryRunSkipsApply(t *testing.T) {
	queryUsersFn = func(_ context.Context, _ *sql.DB) ([]userRef, error) {
		return []userRef{
			{User: "app", Host: "localhost"},
		}, nil
	}
	fetchCreateUserFn = func(_ context.Context, _ *sql.DB, _, _ string) (string, error) {
		return "CREATE USER 'app'@'localhost'", nil
	}
	fetchGrantsFn = func(_ context.Context, _ *sql.DB, _, _ string) ([]string, error) {
		return []string{"GRANT SELECT ON `db`.* TO 'app'@'localhost'"}, nil
	}
	defer func() {
		queryUsersFn = queryUsers
		fetchCreateUserFn = fetchCreateUser
		fetchGrantsFn = fetchGrants
	}()

	source := &sql.DB{}
	dest := &sql.DB{}
	summary, err := CopyUsers(context.Background(), source, dest, CopyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !summary.DryRun {
		t.Error("expected DryRun=true in summary")
	}
	if summary.UsersCopied != 1 {
		t.Errorf("expected 1 user copied (dry-run), got %d", summary.UsersCopied)
	}
	if summary.GrantsCopied != 1 {
		t.Errorf("expected 1 grant counted, got %d", summary.GrantsCopied)
	}
}

func TestCopyUsersSkipsSystemUsers(t *testing.T) {
	queryUsersFn = func(_ context.Context, _ *sql.DB) ([]userRef, error) {
		return []userRef{
			{User: "root", Host: "localhost"},
			{User: "mysql.sys", Host: "localhost"},
			{User: "app", Host: "%"},
		}, nil
	}
	fetchCreateUserFn = func(_ context.Context, _ *sql.DB, _, _ string) (string, error) {
		return "CREATE USER 'app'@'%'", nil
	}
	fetchGrantsFn = func(_ context.Context, _ *sql.DB, _, _ string) ([]string, error) {
		return []string{}, nil
	}
	defer func() {
		queryUsersFn = queryUsers
		fetchCreateUserFn = fetchCreateUser
		fetchGrantsFn = fetchGrants
	}()

	source := &sql.DB{}
	dest := &sql.DB{}
	summary, err := CopyUsers(context.Background(), source, dest, CopyOptions{DryRun: true, Scope: ScopeBusiness})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.UsersFound != 3 {
		t.Errorf("expected 3 users found, got %d", summary.UsersFound)
	}
	if summary.UsersSkipped != 2 {
		t.Errorf("expected 2 system users skipped, got %d", summary.UsersSkipped)
	}
	if summary.UsersCopied != 1 {
		t.Errorf("expected 1 user copied, got %d", summary.UsersCopied)
	}
}
