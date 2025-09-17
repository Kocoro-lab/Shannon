package circuitbreaker

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap/zaptest"
)

func TestDatabaseWrapper_NormalOperations(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewDatabaseWrapper(db, logger)
	ctx := context.Background()

	// Test Ping
	mock.ExpectPing()
	err = wrapper.PingContext(ctx)
	if err != nil {
		t.Errorf("PingContext failed: %v", err)
	}

	// Test Query
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "test").
		AddRow(2, "test2")
	mock.ExpectQuery("SELECT (.+) FROM test").WillReturnRows(rows)

	queryRows, err := wrapper.QueryContext(ctx, "SELECT id, name FROM test")
	if err != nil {
		t.Errorf("QueryContext failed: %v", err)
	}
	defer queryRows.Close()

	// Test Exec
	mock.ExpectExec("INSERT INTO test").
		WithArgs("test").
		WillReturnResult(sqlmock.NewResult(1, 1))

	result, err := wrapper.ExecContext(ctx, "INSERT INTO test (name) VALUES (?)", "test")
	if err != nil {
		t.Errorf("ExecContext failed: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("Expected 1 affected row, got %d", affected)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestDatabaseWrapper_TransactionWrapper(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewDatabaseWrapper(db, logger)
	ctx := context.Background()

	// Test BeginTx
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO test").
		WithArgs("test").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := wrapper.BeginTx(ctx, nil)
	if err != nil {
		t.Errorf("BeginTx failed: %v", err)
	}

	// Test transaction ExecContext
	result, err := tx.ExecContext(ctx, "INSERT INTO test (name) VALUES (?)", "test")
	if err != nil {
		t.Errorf("Transaction ExecContext failed: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("Expected 1 affected row, got %d", affected)
	}

	// Test commit
	err = tx.Commit()
	if err != nil {
		t.Errorf("Transaction Commit failed: %v", err)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestDatabaseWrapper_PreparedStatementWrapper(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewDatabaseWrapper(db, logger)
	ctx := context.Background()

	// Test PrepareContext
	mock.ExpectPrepare("INSERT INTO test").
		ExpectExec().
		WithArgs("test").
		WillReturnResult(sqlmock.NewResult(1, 1))

	stmt, err := wrapper.PrepareContext(ctx, "INSERT INTO test (name) VALUES (?)")
	if err != nil {
		t.Errorf("PrepareContext failed: %v", err)
	}
	defer stmt.Close()

	// Test statement ExecContext
	result, err := stmt.ExecContext(ctx, "test")
	if err != nil {
		t.Errorf("Statement ExecContext failed: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("Expected 1 affected row, got %d", affected)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestDatabaseWrapper_CircuitBreakerTriggering(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewDatabaseWrapper(db, logger)
	ctx := context.Background()

	// Set up expected pings (circuit breaker opens after 5 failures)
	for i := 0; i < 5; i++ {
		mock.ExpectPing().WillReturnError(sql.ErrConnDone)
	}

	// Simulate database failures
	for i := 0; i < 5; i++ {
		err := wrapper.PingContext(ctx)
		if err == nil {
			t.Error("Expected ping to fail")
		}
	}

	// Circuit breaker should be open
	if !wrapper.IsCircuitBreakerOpen() {
		t.Error("Expected circuit breaker to be open after repeated failures")
	}

	// Subsequent calls should fail fast
	err = wrapper.PingContext(ctx)
	if err != ErrCircuitBreakerOpen {
		t.Errorf("Expected circuit breaker open error, got %v", err)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestDatabaseWrapper_QueryRowContextCB(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewDatabaseWrapper(db, logger)
	ctx := context.Background()

	// Test normal QueryRowContextCB
	rows := sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "test")
	mock.ExpectQuery("SELECT (.+) FROM test WHERE id = \\?").
		WithArgs(1).
		WillReturnRows(rows)

	row, err := wrapper.QueryRowContextCB(ctx, "SELECT id, name FROM test WHERE id = ?", 1)
	if err != nil {
		t.Errorf("QueryRowContextCB failed: %v", err)
	}

	var id int
	var name string
	err = row.Scan(&id, &name)
	if err != nil {
		t.Errorf("Row scan failed: %v", err)
	}

	if id != 1 || name != "test" {
		t.Errorf("Expected id=1, name='test', got id=%d, name='%s'", id, name)
	}

	// Test circuit breaker error propagation
	// Create a separate mock with ping monitoring for circuit breaker test
	dbForCB, mockForCB, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("Failed to create sqlmock for circuit breaker test: %v", err)
	}
	defer dbForCB.Close()

	wrapperForCB := NewDatabaseWrapper(dbForCB, logger)

	// Set up expected pings (circuit breaker opens after 5 failures)
	for i := 0; i < 5; i++ {
		mockForCB.ExpectPing().WillReturnError(sql.ErrConnDone)
	}

	// First trip the circuit breaker
	for i := 0; i < 5; i++ {
		wrapperForCB.PingContext(ctx)
	}

	// Now test QueryRowContextCB with circuit breaker open
	row, err = wrapperForCB.QueryRowContextCB(ctx, "SELECT id FROM test", 1)
	if err != ErrCircuitBreakerOpen {
		t.Errorf("Expected circuit breaker open error, got %v", err)
	}
	if row != nil {
		t.Error("Expected nil row when circuit breaker is open")
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
	if err := mockForCB.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled circuit breaker expectations: %v", err)
	}
}
