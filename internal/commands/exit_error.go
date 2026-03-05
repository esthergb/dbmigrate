package commands

import "errors"

const (
	// ExitCodeDiff marks successful command execution with detected incompatibilities/differences.
	ExitCodeDiff = 2
	// ExitCodeVerifyFailed marks verify command internal/runtime failures.
	ExitCodeVerifyFailed = 4
)

// ExitError carries a command-specific process exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return "command failed"
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// WithExitCode annotates an error with a specific process exit code.
func WithExitCode(code int, err error) error {
	if err == nil {
		return nil
	}

	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return err
	}
	return &ExitError{
		Code: code,
		Err:  err,
	}
}

// ResolveExitCode extracts an explicit exit code from err.
func ResolveExitCode(err error) (int, bool) {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code, true
	}
	return 0, false
}
