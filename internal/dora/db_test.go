package dora

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		expectErr bool
	}{
		{name: "trims and lowercases", input: " 0xABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD ", want: "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"},
		{name: "adds prefix when missing", input: "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD", want: "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"},
		{name: "invalid length", input: "0x1234", expectErr: true},
		{name: "invalid hex", input: "0xzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", expectErr: true},
		{name: "empty", input: "   ", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeAddress(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeAddress(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestOrderHelpers(t *testing.T) {
	validSort := []string{"depositor_address", "withdrawal_address", "validators_total", "slashed", "voluntary_exited", "active", "total_active_effective_balance"}
	for _, field := range validSort {
		if got := OrderBy(field); got != field {
			t.Fatalf("OrderBy(%q) = %s, want %s", field, got, field)
		}
	}
	if got := OrderBy("unknown"); got != "total_deposit" {
		t.Fatalf("OrderBy default = %s, want total_deposit", got)
	}

	if got := OrderDirection("asc"); got != "ASC" {
		t.Fatalf("OrderDirection asc = %s, want ASC", got)
	}
	if got := OrderDirection("DESC"); got != "DESC" {
		t.Fatalf("OrderDirection DESC = %s, want DESC", got)
	}
}

func TestEpochConversionsRoundTrip(t *testing.T) {
	epochs := []uint64{0, 1, 12345, epochShift, epochShift + 1}
	for _, epoch := range epochs {
		stored := convertUint64EpochToStorage(epoch)
		restored := ConvertInt64ToUint64(stored)
		if restored != epoch {
			t.Fatalf("round trip failed for %d: got %d", epoch, restored)
		}
	}
}

func TestQueryStatsUsesDefaultLimitAndScan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	rows := sqlmock.NewRows([]string{"value"}).AddRow(5)
	mock.ExpectQuery("SELECT \\$1").WithArgs(100).WillReturnRows(rows)

	stats, err := queryStats[int](context.Background(), db, 0, "SELECT $1", func(rows *sql.Rows, out *int) error {
		return rows.Scan(out)
	})
	if err != nil {
		t.Fatalf("queryStats returned error: %v", err)
	}
	if len(stats) != 1 || stats[0] != 5 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestQueryStatsPropagatesScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	rows := sqlmock.NewRows([]string{"value"}).AddRow("not-an-int")
	mock.ExpectQuery("SELECT value FROM test").WithArgs(5).WillReturnRows(rows)

	_, err = queryStats[int](context.Background(), db, 5, "SELECT value FROM test", func(rows *sql.Rows, out *int) error {
		return rows.Scan(out)
	})
	if err == nil {
		t.Fatalf("expected error but got none")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
