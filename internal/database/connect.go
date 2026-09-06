package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func Config(name string, admin bool) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig("host=db port=5432 sslmode=verify-full sslrootcert=/run/api/ca.crt connect_timeout=5")
	if err != nil {
		return nil, err
	}
	cfg.Database = name
	cfg.User = "proof_api"
	path := "/run/api/password"
	if admin {
		cfg.User = "postgres"
		path = "/run/admin/password"
	}
	password, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read credential file: %w", err)
	}
	cfg.Password = strings.TrimSpace(string(password))
	cfg.RuntimeParams["application_name"] = "cleanup-receipt-dbtool"
	return cfg, nil
}

func Open(ctx context.Context, name string, admin bool) (*sql.DB, error) {
	cfg, err := Config(name, admin)
	if err != nil {
		return nil, err
	}
	conn := stdlib.OpenDB(*cfg)
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(2)
	conn.SetConnMaxLifetime(5 * time.Minute)
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
