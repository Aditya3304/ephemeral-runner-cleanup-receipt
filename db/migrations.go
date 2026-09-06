package db

import (
	"context"
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate uses Goose's transaction and version tracking with embedded SQL.
// Commands run in a one-shot administrative process, never in the CI job.
func Migrate(ctx context.Context, conn *sql.DB, command string) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.RunContext(ctx, command, conn, "migrations")
}
