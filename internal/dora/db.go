package dora

import (
	"context"
	"database/sql"
	"endurance-rewards/internal/config"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

const defaultStatsLimit = 100

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
	ValidatorStatus
}

// DepositorStat represents aggregated deposits for the depositor (tx sender) address.
type DepositorStat struct {
	DepositorAddress string `json:"depositor_address"`
	DepositorLabel   string `json:"depositor_label,omitempty"`
	ValidatorStatus
}

// ValidatorStatus captures validator status counts shared by depositor/withdrawal stats.
type ValidatorStatus struct {
	TotalDeposit    int64 `json:"total_deposit"`
	ValidatorsTotal int64 `json:"validators_total"`
	Slashed         int64 `json:"slashed"`
	VoluntaryExited int64 `json:"voluntary_exited"`
	Active          int64 `json:"active"`
}

// TopWithdrawalAddresses aggregates deposits by normalized withdrawal address and returns top N by amount.
//
// Normalization: for withdrawal credentials with prefix 0x01 or 0x02, the execution-layer address is stored
// in the last 20 bytes of the 32-byte credentials. We group by those last 20 bytes regardless of prefix
// to treat 0x01 and 0x02 as the same address.
func (d *DB) TopWithdrawalAddresses(ctx context.Context, limit int, sortBy string, order string) ([]WithdrawalStat, error) {
	const baseQuery = `
SELECT
  '0x' || encode(substr(v.withdrawal_credentials, 13, 20), 'hex') AS withdrawal_address,
  COALESCE(SUM(d.amount), 0)::bigint AS total_deposit,
  COUNT(DISTINCT v.validator_index) AS validators_total,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE v.slashed) AS slashed,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE NOT v.slashed AND v.effective_balance = 0) AS voluntary_exited,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE NOT v.slashed AND v.effective_balance > 0) AS active
FROM validators v  left join deposits d on v.pubkey  = d.publickey 
GROUP BY withdrawal_address
ORDER BY %s %s
LIMIT $1`

	q := fmt.Sprintf(baseQuery, OrderBy(sortBy), OrderDirection(order))

	return queryStats(ctx, d.db, limit, q, func(rows *sql.Rows, stat *WithdrawalStat) error {
		return rows.Scan(
			&stat.WithdrawalAddress,
			&stat.TotalDeposit,
			&stat.ValidatorsTotal,
			&stat.Slashed,
			&stat.VoluntaryExited,
			&stat.Active,
		)
	})
}

// TopDepositorAddresses aggregates deposits by transaction sender and returns top N by validator count.
func (d *DB) TopDepositorAddresses(ctx context.Context, limit int, sortBy string, order string) ([]DepositorStat, error) {
	const baseQuery = `
SELECT
  '0x' || encode(dt.tx_sender,'hex') as depositor_address,
  SUM(dt.amount)::bigint AS total_deposit,
  COUNT(DISTINCT v.validator_index) AS validators_total,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE v.slashed) AS slashed,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE NOT v.slashed AND v.effective_balance = 0) AS voluntary_exited,
  COUNT(DISTINCT v.validator_index) FILTER (WHERE NOT v.slashed AND v.effective_balance > 0) AS active
FROM deposit_txs dt 
LEFT JOIN validators v ON dt.publickey = v.pubkey 
GROUP BY depositor_address
ORDER BY %s %s
LIMIT $1`

	q := fmt.Sprintf(baseQuery, OrderBy(sortBy), OrderDirection(order))

	return queryStats(ctx, d.db, limit, q, func(rows *sql.Rows, stat *DepositorStat) error {
		return rows.Scan(
			&stat.DepositorAddress,
			&stat.TotalDeposit,
			&stat.ValidatorsTotal,
			&stat.Slashed,
			&stat.VoluntaryExited,
			&stat.Active,
		)
	})
}

func OrderBy(sortBy string) string {
	switch sortBy {
	case "depositor_address", "withdrawal_address", "validators_total", "slashed", "voluntary_exited", "active", "total_amount":
		return sortBy
	default:
		return "total_deposit"
	}
}

func OrderDirection(order string) string {
	switch strings.ToLower(order) {
	case "asc":
		return "ASC"
	default:
		return "DESC"
	}
}

func queryStats[T any](ctx context.Context, db *sql.DB, limit int, query string, scan func(*sql.Rows, *T) error) ([]T, error) {
	if limit <= 0 {
		limit = defaultStatsLimit
	}

	rows, err := db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]T, 0, limit)
	for rows.Next() {
		var item T
		if err := scan(rows, &item); err != nil {
			return nil, err
		}
		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// EffectiveBalances returns the effective_balance for the requested validator indices.
func (d *DB) EffectiveBalances(ctx context.Context, indices []uint64) (map[uint64]int64, error) {
	if d == nil || d.db == nil || len(indices) == 0 {
		return map[uint64]int64{}, nil
	}

	unique := make(map[uint64]struct{}, len(indices))
	ids := make([]int64, 0, len(indices))
	for _, idx := range indices {
		if _, exists := unique[idx]; exists {
			continue
		}
		unique[idx] = struct{}{}
		ids = append(ids, int64(idx))
	}

	rows, err := d.db.QueryContext(ctx, `
SELECT validator_index, effective_balance
FROM validators
WHERE validator_index = ANY($1)
`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	balances := make(map[uint64]int64, len(ids))
	for rows.Next() {
		var idx int64
		var balance int64
		if err := rows.Scan(&idx, &balance); err != nil {
			return nil, err
		}
		balances[uint64(idx)] = balance
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return balances, nil
}
