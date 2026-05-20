//go:build !js

package sqldb

import (
	"fmt"
	"path/filepath"

	sqldbv2 "github.com/lightningnetwork/lnd/sqldb/v2"
)

func openSQLStore(dataDir string, cfg *Config) (sqldbv2.DB, error) {
	switch cfg.Backend {
	case BackendSqlite:
		dbPath := filepath.Join(dataDir, cfg.sqliteFilename())
		s, err := sqldbv2.NewSqliteStore(cfg.Sqlite, dbPath)
		if err != nil {
			return nil, fmt.Errorf("sqldb: open sqlite: %w", err)
		}

		return s, nil

	case BackendPostgres:
		s, err := sqldbv2.NewPostgresStore(cfg.Postgres)
		if err != nil {
			return nil, fmt.Errorf("sqldb: open postgres: %w", err)
		}

		return s, nil

	default:
		return nil, fmt.Errorf("sqldb: unknown backend %d", cfg.Backend)
	}
}
