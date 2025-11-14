package dora

import (
	"context"
	"database/sql"
	"endurance-rewards/internal/config"
	"fmt"
)

// DB wraps a sql.DB for the Dora Postgres database.
type DB struct {
	db *sql.DB
}

// New creates a new DB connection using the provided config.
func New(cfg *config.Config) (*DB, error) {
	dsn := cfg.DoraPGURL
	if dsn == "" {
		return nil, fmt.Errorf("DoraPGURL is empty")
	}

	// The driver name "postgres" requires that a PostgreSQL driver is linked via blank import.
	// We don't add the driver here to avoid extra module requirements in environments without network access.
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	// Validate DSN (this will still fail if the driver is not linked at runtime)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

// Close closes the database connection.
func (d *DB) Close() {
	if d != nil && d.db != nil {
		_ = d.db.Close()
	}
}

// WithdrawalStat represents aggregated deposits for a withdrawal address.
type WithdrawalStat struct {
	Address     string `json:"address"`
	TotalAmount int64  `json:"total_amount"`
}

// TopWithdrawalAddresses aggregates deposits by normalized withdrawal address and returns top N by amount.
//
// Normalization: for withdrawal credentials with prefix 0x01 or 0x02, the execution-layer address is stored
// in the last 20 bytes of the 32-byte credentials. We group by those last 20 bytes regardless of prefix
// to treat 0x01 and 0x02 as the same address.
func (d *DB) TopWithdrawalAddresses(ctx context.Context, limit int) ([]WithdrawalStat, error) {
	if limit <= 0 {
		limit = 100
	}

	const q = `
SELECT
  '0x' || encode(substr(withdrawalcredentials, 13, 20), 'hex') AS address,
  SUM(amount)::bigint AS total_amount
FROM deposits
WHERE get_byte(withdrawalcredentials, 0) IN (1, 2)
GROUP BY address
ORDER BY total_amount DESC
LIMIT $1`

	rows, err := d.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]WithdrawalStat, 0, limit)
	for rows.Next() {
		var s WithdrawalStat
		if err := rows.Scan(&s.Address, &s.TotalAmount); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
