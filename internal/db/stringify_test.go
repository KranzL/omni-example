package db

import (
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestStringifyNumericAndTime(t *testing.T) {
	n := pgtype.Numeric{Int: big.NewInt(38266562), Exp: -2, Valid: true}
	if got := stringify(n); got != "382665.62" {
		t.Fatalf("numeric = %q, want 382665.62", got)
	}
	whole := pgtype.Numeric{Int: big.NewInt(12345), Exp: 0, Valid: true}
	if got := stringify(whole); got != "12345" {
		t.Fatalf("whole numeric = %q, want 12345", got)
	}
	null := pgtype.Numeric{}
	if got := stringify(null); got != "" {
		t.Fatalf("null numeric = %q, want empty", got)
	}
	ts := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	if got := stringify(ts); got != "2024-03-01 00:00:00" {
		t.Fatalf("time = %q, want 2024-03-01 00:00:00", got)
	}
	if got := stringify(382665.6215791702); got != "382665.6215791702" {
		t.Fatalf("float = %q", got)
	}
	if got := stringify(int64(7)); got != "7" {
		t.Fatalf("int = %q", got)
	}
}

func TestStringifyLargeFloatArrayUUID(t *testing.T) {
	if got := stringify(1838928.5062675476); got != "1838928.5062675476" {
		t.Fatalf("large float = %q, want 1838928.5062675476", got)
	}
	if got := stringify(float32(2.5e7)); got != "25000000" {
		t.Fatalf("float32 = %q, want 25000000", got)
	}
	if got := stringify([]any{"New York", "LA", nil}); got != "{New York,LA,}" {
		t.Fatalf("array = %q", got)
	}
	id := [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	if got := stringify(id); got != "12345678-9abc-def0-0123-456789abcdef" {
		t.Fatalf("uuid = %q", got)
	}
}
