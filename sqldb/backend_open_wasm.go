//go:build js && wasm

package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	sqldbv2 "github.com/lightningnetwork/lnd/sqldb/v2"
	_ "github.com/sputn1ck/go-wasmsqlite"
)

type wasmStore struct {
	*sqldbv2.BaseDB
}

var _ sqldbv2.DB = (*wasmStore)(nil)

func openSQLStore(_ string, cfg *Config) (sqldbv2.DB, error) {
	if cfg.Backend != BackendSqlite {
		return nil, fmt.Errorf("sqldb: wasm only supports sqlite")
	}

	dsn := fmt.Sprintf(
		"file=/%s?vfs=opfs&busy_timeout=%d&mode=rwc&parse_time=true",
		cfg.sqliteFilename(), cfg.Sqlite.BusyTimeout.Milliseconds(),
	)
	if cfg.Sqlite.BusyTimeout == 0 {
		dsn = fmt.Sprintf(
			"file=/%s?vfs=opfs&busy_timeout=%d&mode=rwc&parse_time=true",
			cfg.sqliteFilename(), sqldbv2.DefaultSqliteBusyTimeout.Milliseconds(),
		)
	}

	db, err := sql.Open("wasmsqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqldb: open wasm sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return &wasmStore{
		BaseDB: &sqldbv2.BaseDB{
			DB:          db,
			BackendType: sqldbv2.BackendTypeSqlite,
		},
	}, nil
}

func (s *wasmStore) GetBaseDB() *sqldbv2.BaseDB {
	return s.BaseDB
}

func (s *wasmStore) ExecuteMigrations(set sqldbv2.MigrationSet) error {
	if s.SkipMigrations {
		return nil
	}

	ctx := context.Background()
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	entries, err := fs.ReadDir(set.SQLFiles, set.SQLFileDirectory)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}

		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.ToSlash(filepath.Join(set.SQLFileDirectory, name))
		migration, err := set.SQLFiles.ReadFile(path)
		if err != nil {
			return err
		}

		statements := migrationStatements(string(migration))
		if len(statements) == 0 {
			continue
		}

		for _, stmt := range statements {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("run wasm migration %s: %w", name, err)
			}
		}
	}

	return s.verifySchema(ctx, conn)
}

func (s *wasmStore) verifySchema(ctx context.Context, conn *sql.Conn) error {
	var name string
	err := conn.QueryRowContext(
		ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?",
		"regular_filters",
	).Scan(&name)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("verify wasm schema: %w", err)
	}

	rows, err := conn.QueryContext(
		ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name",
	)
	if err != nil {
		return fmt.Errorf("verify wasm schema: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("verify wasm schema: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("verify wasm schema: %w", err)
	}

	return fmt.Errorf(
		"verify wasm schema: regular_filters table missing after "+
			"migrations; tables=%v", tables,
	)
}

func migrationStatements(sql string) []string {
	parts := strings.Split(sql, ";")
	statements := make([]string, 0, len(parts))

	for _, part := range parts {
		stmt := strings.TrimSpace(stripSQLLineComments(part))
		if stmt == "" {
			continue
		}

		statements = append(statements, stmt)
	}

	return statements
}

func stripSQLLineComments(stmt string) string {
	lines := make([]string, 0, strings.Count(stmt, "\n")+1)
	for _, line := range strings.Split(stmt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}
