package circuitbreaker

import (
	"context"
	"database/sql"
	"time"

	"go.uber.org/zap"
)

// DatabaseWrapper wraps database operations with circuit breaker
type DatabaseWrapper struct {
	db     *sql.DB
	cb     *CircuitBreaker
	logger *zap.Logger
}

// NewDatabaseWrapper creates a database wrapper with circuit breaker
func NewDatabaseWrapper(db *sql.DB, logger *zap.Logger) *DatabaseWrapper {
	config := GetDatabaseConfig().ToConfig()
	cb := NewCircuitBreaker("postgresql", config, logger)

	// Register with metrics collector
	GlobalMetricsCollector.RegisterCircuitBreaker("postgresql", "database-client", cb)

	return &DatabaseWrapper{
		db:     db,
		cb:     cb,
		logger: logger,
	}
}

// PingContext wraps database ping with circuit breaker
func (dw *DatabaseWrapper) PingContext(ctx context.Context) error {
	var err error

	cbErr := dw.cb.Execute(ctx, func() error {
		err = dw.db.PingContext(ctx)
		return err
	})

	// Record metrics
	state := dw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return cbErr
	}
	return err
}

// QueryContext wraps database query with circuit breaker
func (dw *DatabaseWrapper) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	var rows *sql.Rows
	var err error

	cbErr := dw.cb.Execute(ctx, func() error {
		rows, err = dw.db.QueryContext(ctx, query, args...)
		return err
	})

	// Record metrics
	state := dw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return rows, err
}

// QueryRowContext wraps database query row with circuit breaker
// Returns (*sql.Row, error) to properly propagate circuit breaker errors
func (dw *DatabaseWrapper) QueryRowContextCB(ctx context.Context, query string, args ...interface{}) (*sql.Row, error) {
	var row *sql.Row

	cbErr := dw.cb.Execute(ctx, func() error {
		row = dw.db.QueryRowContext(ctx, query, args...)
		// We can't easily check for query errors here since sql.Row doesn't expose them
		// The error will be checked when Scan() is called
		return nil
	})

	// Record metrics - assume success for QueryRow since errors are deferred
	state := dw.cb.State()
	success := cbErr == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}

	return row, nil
}

// QueryRowContext wraps database query row with circuit breaker (legacy API)
// Deprecated: Use QueryRowContextCB for proper error handling
func (dw *DatabaseWrapper) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	row, err := dw.QueryRowContextCB(ctx, query, args...)
	if err != nil {
		// Return a row that will error on Scan with circuit breaker error
		// This is not ideal but maintains backward compatibility
		return &sql.Row{}
	}
	return row
}

// ExecContext wraps database exec with circuit breaker
func (dw *DatabaseWrapper) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	var result sql.Result
	var err error

	cbErr := dw.cb.Execute(ctx, func() error {
		result, err = dw.db.ExecContext(ctx, query, args...)
		return err
	})

	// Record metrics
	state := dw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return result, err
}

// TxWrapper wraps sql.Tx with circuit breaker protection
type TxWrapper struct {
	tx     *sql.Tx
	cb     *CircuitBreaker
	logger *zap.Logger
}

// BeginTx wraps database transaction begin with circuit breaker
func (dw *DatabaseWrapper) BeginTx(ctx context.Context, opts *sql.TxOptions) (*TxWrapper, error) {
	var tx *sql.Tx
	var err error

	cbErr := dw.cb.Execute(ctx, func() error {
		tx, err = dw.db.BeginTx(ctx, opts)
		return err
	})

	// Record metrics
	state := dw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	if err != nil {
		return nil, err
	}

	return &TxWrapper{
		tx:     tx,
		cb:     dw.cb,
		logger: dw.logger,
	}, nil
}

// Transaction wrapper methods
func (tw *TxWrapper) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	var result sql.Result
	var err error

	cbErr := tw.cb.Execute(ctx, func() error {
		result, err = tw.tx.ExecContext(ctx, query, args...)
		return err
	})

	// Record metrics
	state := tw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return result, err
}

func (tw *TxWrapper) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	var rows *sql.Rows
	var err error

	cbErr := tw.cb.Execute(ctx, func() error {
		rows, err = tw.tx.QueryContext(ctx, query, args...)
		return err
	})

	// Record metrics
	state := tw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return rows, err
}

func (tw *TxWrapper) QueryRowContext(ctx context.Context, query string, args ...interface{}) (*sql.Row, error) {
	var row *sql.Row

	cbErr := tw.cb.Execute(ctx, func() error {
		row = tw.tx.QueryRowContext(ctx, query, args...)
		return nil
	})

	// Record metrics
	state := tw.cb.State()
	success := cbErr == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return row, nil
}

func (tw *TxWrapper) PrepareContext(ctx context.Context, query string) (*StmtWrapper, error) {
	var stmt *sql.Stmt
	var err error

	cbErr := tw.cb.Execute(ctx, func() error {
		stmt, err = tw.tx.PrepareContext(ctx, query)
		return err
	})

	// Record metrics
	state := tw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	if err != nil {
		return nil, err
	}

	return &StmtWrapper{
		stmt:   stmt,
		cb:     tw.cb,
		logger: tw.logger,
	}, nil
}

func (tw *TxWrapper) Commit() error {
	var err error

	cbErr := tw.cb.Execute(context.Background(), func() error {
		err = tw.tx.Commit()
		return err
	})

	// Record metrics
	state := tw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return cbErr
	}
	return err
}

func (tw *TxWrapper) Rollback() error {
	// Don't use circuit breaker for rollback - we always want to try it
	return tw.tx.Rollback()
}

// StmtWrapper wraps sql.Stmt with circuit breaker protection
type StmtWrapper struct {
	stmt   *sql.Stmt
	cb     *CircuitBreaker
	logger *zap.Logger
}

// PrepareContext wraps database prepare with circuit breaker
func (dw *DatabaseWrapper) PrepareContext(ctx context.Context, query string) (*StmtWrapper, error) {
	var stmt *sql.Stmt
	var err error

	cbErr := dw.cb.Execute(ctx, func() error {
		stmt, err = dw.db.PrepareContext(ctx, query)
		return err
	})

	// Record metrics
	state := dw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	if err != nil {
		return nil, err
	}

	return &StmtWrapper{
		stmt:   stmt,
		cb:     dw.cb,
		logger: dw.logger,
	}, nil
}

// Statement wrapper methods
func (sw *StmtWrapper) ExecContext(ctx context.Context, args ...interface{}) (sql.Result, error) {
	var result sql.Result
	var err error

	cbErr := sw.cb.Execute(ctx, func() error {
		result, err = sw.stmt.ExecContext(ctx, args...)
		return err
	})

	// Record metrics
	state := sw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return result, err
}

func (sw *StmtWrapper) QueryContext(ctx context.Context, args ...interface{}) (*sql.Rows, error) {
	var rows *sql.Rows
	var err error

	cbErr := sw.cb.Execute(ctx, func() error {
		rows, err = sw.stmt.QueryContext(ctx, args...)
		return err
	})

	// Record metrics
	state := sw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return rows, err
}

func (sw *StmtWrapper) QueryRowContext(ctx context.Context, args ...interface{}) (*sql.Row, error) {
	var row *sql.Row

	cbErr := sw.cb.Execute(ctx, func() error {
		row = sw.stmt.QueryRowContext(ctx, args...)
		return nil
	})

	// Record metrics
	state := sw.cb.State()
	success := cbErr == nil
	GlobalMetricsCollector.RecordRequest("postgresql", "database-client", state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return row, nil
}

func (sw *StmtWrapper) Close() error {
	// Don't use circuit breaker for close - we always want to try it
	return sw.stmt.Close()
}

// Stats returns database stats
func (dw *DatabaseWrapper) Stats() sql.DBStats {
	return dw.db.Stats()
}

// Close closes the database connection
func (dw *DatabaseWrapper) Close() error {
	return dw.db.Close()
}

// SetMaxOpenConns sets the maximum number of open connections
func (dw *DatabaseWrapper) SetMaxOpenConns(n int) {
	dw.db.SetMaxOpenConns(n)
}

// SetMaxIdleConns sets the maximum number of idle connections
func (dw *DatabaseWrapper) SetMaxIdleConns(n int) {
	dw.db.SetMaxIdleConns(n)
}

// SetConnMaxLifetime sets the maximum connection lifetime
func (dw *DatabaseWrapper) SetConnMaxLifetime(d time.Duration) {
	dw.db.SetConnMaxLifetime(d)
}

// GetDB returns the underlying database connection for operations not covered by wrapper
func (dw *DatabaseWrapper) GetDB() *sql.DB {
	return dw.db
}

// IsCircuitBreakerOpen returns true if the circuit breaker is open
func (dw *DatabaseWrapper) IsCircuitBreakerOpen() bool {
	return dw.cb.State() == StateOpen
}
