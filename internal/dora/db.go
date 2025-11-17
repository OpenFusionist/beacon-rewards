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
	WithdrawalAddress string `json:"withdrawal_address"`
	TotalAmount       int64  `json:"total_amount"`
	ValidatorsTotal   int64  `json:"validators_total"`
	Slashed           int64  `json:"slashed"`
	VoluntaryExited   int64  `json:"voluntary_exited"`
	Active            int64  `json:"active"`
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
  '0x' || encode(substr(v.withdrawal_credentials, 13, 20), 'hex') AS withdrawal_address,
  COALESCE(SUM(d.amount), 0)::bigint AS total_amount,
  COUNT(DISTINCT v.validator_index) AS validators_total,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE v.slashed) AS slashed,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE NOT v.slashed AND v.effective_balance = 0) AS voluntary_exited,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE NOT v.slashed AND v.effective_balance > 0) AS active
FROM validators v  left join deposits d on v.pubkey  = d.publickey 
GROUP BY withdrawal_address
ORDER BY validators_total DESC
LIMIT $1`

	rows, err := d.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]WithdrawalStat, 0, limit)
	for rows.Next() {
		var s WithdrawalStat
		if err := rows.Scan(&s.WithdrawalAddress, &s.TotalAmount, &s.ValidatorsTotal, &s.Slashed, &s.VoluntaryExited, &s.Active); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
