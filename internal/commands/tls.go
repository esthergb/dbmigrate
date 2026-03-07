package commands

import (
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/db"
)

func tlsOptionsFromRuntime(cfg config.RuntimeConfig) db.TLSOptions {
	return db.TLSOptions{
		Mode:     cfg.TLSMode,
		CAFile:   cfg.CAFile,
		CertFile: cfg.CertFile,
		KeyFile:  cfg.KeyFile,
	}
}
