package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ApplyDDL выполняет map[fqn]sql. Ожидается idempotent DDL (create ... if not exists).
func ApplyDDL(db *sql.DB, ddl map[string]string) error {
	// стабильно: по имени сущности
	keys := make([]string, 0, len(ddl))
	for k := range ddl {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, k := range keys {
		sqlText := strings.TrimSpace(ddl[k])
		if sqlText == "" {
			continue
		}
		// [1.2] internal/pg/apply.go — игнорируем duplicate_object (42710)
		if _, err := db.ExecContext(ctx, sqlText); err != nil {
			// pgx/stdlib возвращает *pgconn.PgError
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "42710" {
				log.Printf("DDL skipped (already exists): %s (%s)", pgErr.ConstraintName, strings.TrimSpace(pgErr.Message))
				continue
			}
			// подстраховка по фразе (на случай других объектов)
			e := strings.ToLower(err.Error())
			if strings.Contains(e, "already exists") || strings.Contains(e, "duplicate") {
				log.Printf("DDL skipped (already exists): %v", err)
				continue
			}
			return fmt.Errorf("DDL apply failed: %w", err)
		}
	}
	return nil
}
