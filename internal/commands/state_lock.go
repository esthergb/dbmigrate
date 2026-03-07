package commands

import (
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/state"
)

func withStateDirLock(cfg config.RuntimeConfig, fn func() error) error {
	lock, err := state.AcquireDirLock(cfg.StateDir)
	if err != nil {
		return WithExitCode(ExitCodeDiff, err)
	}
	defer func() {
		_ = lock.Release()
	}()
	return fn()
}
