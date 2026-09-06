package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/db"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/database"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/dbfixture"
)

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: dbtool migrate|status|demo")
	}
	command := os.Args[1]
	if command != "migrate" && command != "status" && command != "demo" {
		return fmt.Errorf("unknown command %q", command)
	}
	conn, err := database.Open(ctx, "proof", command != "demo")
	if err != nil {
		return err
	}
	defer conn.Close()
	if command == "migrate" {
		return db.Migrate(ctx, conn, "up")
	}
	if command == "demo" {
		fmt.Println("SCHEMA DEMO — synthetic metadata, not a signed receipt or cleanup demonstration.")
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		id, err := dbfixture.Insert(ctx, tx, fmt.Sprintf("demo-%d", time.Now().UnixNano()), "fail")
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, dbfixture.IncidentSQL, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
			return err
		}
		var verdict, state string
		if err = tx.QueryRowContext(ctx, `SELECT r.verdict,i.state FROM evidence.receipts r JOIN evidence.incidents i ON i.receipt_id=r.id WHERE r.id=$1`, id).Scan(&verdict, &state); err != nil {
			return err
		}
		fmt.Printf("Inside real PostgreSQL transaction: verdict=%s incident=%s; deferred constraints accepted the pair.\n", verdict, state)
		if err = tx.Rollback(); err != nil {
			return err
		}
		var remaining int
		if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM evidence.receipts WHERE id=$1", id).Scan(&remaining); err != nil {
			return err
		}
		if remaining != 0 {
			return fmt.Errorf("demo rollback did not remove fixture")
		}
		fmt.Println("Rolled back: no synthetic receipt or incident persisted.")
		return nil
	}
	var version, tlsVersion string
	var ssl bool
	var schema, receipts, incidents, attempts int
	if err = conn.QueryRowContext(ctx, `SELECT current_setting('server_version'), ssl, version FROM pg_stat_ssl WHERE pid=pg_backend_pid()`).Scan(&version, &ssl, &tlsVersion); err != nil {
		return err
	}
	if err = conn.QueryRowContext(ctx, "SELECT COALESCE(max(version_id),0) FROM goose_db_version WHERE is_applied").Scan(&schema); err != nil {
		return err
	}
	if err = conn.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM evidence.receipts),(SELECT count(*) FROM evidence.incidents),(SELECT count(*) FROM evidence.finalization_attempts)`).Scan(&receipts, &incidents, &attempts); err != nil {
		return err
	}
	fmt.Printf("PostgreSQL %s | TLS=%t (%s, verify-full) | Goose schema=%d\n", version, ssl, tlsVersion, schema)
	fmt.Printf("Stored receipts=%d | incidents=%d | finalization attempts=%d\n", receipts, incidents, attempts)
	fmt.Println("Milestone a foundation only. Cleanup, signing, ingestion API and dashboard are not implemented yet.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
