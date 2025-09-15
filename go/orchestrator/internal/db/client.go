package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"go.uber.org/zap"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
)

// Config holds database configuration
type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	MaxConnections  int
	IdleConnections int
	MaxLifetime     time.Duration
	SSLMode         string
}

// Client manages database connections and operations
type Client struct {
	db     *circuitbreaker.DatabaseWrapper
	logger *zap.Logger
	config *Config

	// Write queue for async operations
	writeQueue chan WriteRequest
	workers    int
	stopCh     chan struct{}
	workerWg   sync.WaitGroup // Track worker goroutines for graceful shutdown
}

// WriteRequest represents an async write operation
type WriteRequest struct {
	Type     WriteType
	Data     interface{}
	Callback func(error)
}

type WriteType int

const (
	WriteTypeTaskExecution WriteType = iota
	WriteTypeAgentExecution
	WriteTypeToolExecution
	WriteTypeSessionArchive
	WriteTypeAuditLog
	WriteTypeBatch
)

// String returns the string representation of WriteType
func (wt WriteType) String() string {
	switch wt {
	case WriteTypeTaskExecution:
		return "TaskExecution"
	case WriteTypeAgentExecution:
		return "AgentExecution"
	case WriteTypeToolExecution:
		return "ToolExecution"
	case WriteTypeSessionArchive:
		return "SessionArchive"
	case WriteTypeAuditLog:
		return "AuditLog"
	case WriteTypeBatch:
		return "Batch"
	default:
		return "Unknown"
	}
}

// NewClient creates a new database client with connection pool
func NewClient(config *Config, logger *zap.Logger) (*Client, error) {
	if config.MaxConnections == 0 {
		config.MaxConnections = 25
	}
	if config.IdleConnections == 0 {
		config.IdleConnections = 5
	}
	if config.MaxLifetime == 0 {
		config.MaxLifetime = 5 * time.Minute
	}
	if config.SSLMode == "" {
		config.SSLMode = "require"
	}

	// Build connection string
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.Database, config.SSLMode,
	)

	// Open database connection
	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	rawDB.SetMaxOpenConns(config.MaxConnections)
	rawDB.SetMaxIdleConns(config.IdleConnections)
	rawDB.SetConnMaxLifetime(config.MaxLifetime)

	// Create circuit breaker wrapped database
	db := circuitbreaker.NewDatabaseWrapper(rawDB, logger)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		rawDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	client := &Client{
		db:         db,
		logger:     logger,
		config:     config,
		writeQueue: make(chan WriteRequest, 1000), // Buffer size of 1000
		workers:    10,                            // Default 10 workers
		stopCh:     make(chan struct{}),
	}

	// Start async workers
	client.startWorkers()

	// Start health check routine
	go client.healthCheck()

	logger.Info("Database client initialized",
		zap.String("host", config.Host),
		zap.Int("max_connections", config.MaxConnections),
		zap.Int("workers", client.workers),
	)

	return client, nil
}

// startWorkers initializes the worker pool for async writes
func (c *Client) startWorkers() {
	for i := 0; i < c.workers; i++ {
		c.workerWg.Add(1)
		go c.writeWorker(i)
	}
}

// writeWorker processes write requests from the queue
func (c *Client) writeWorker(id int) {
	c.logger.Debug("Write worker started", zap.Int("worker_id", id))

	// Batch buffer for batch writes
	batchBuffer := make([]WriteRequest, 0, 100)
	batchTicker := time.NewTicker(1 * time.Second)
	defer batchTicker.Stop()

	for {
		select {
		case <-c.stopCh:
			// Drain remaining requests
			c.drainQueue(batchBuffer)
			c.logger.Info("Write worker stopped", zap.Int("worker_id", id))
			c.workerWg.Done()
			return

		case req := <-c.writeQueue:
			// Handle different write types
			switch req.Type {
			case WriteTypeBatch:
				batchBuffer = append(batchBuffer, req)
				if len(batchBuffer) >= 100 {
					c.processBatch(batchBuffer)
					batchBuffer = batchBuffer[:0]
				}
			default:
				// Process immediately
				c.processWrite(req)
			}

		case <-batchTicker.C:
			// Flush batch buffer periodically
			if len(batchBuffer) > 0 {
				c.processBatch(batchBuffer)
				batchBuffer = batchBuffer[:0]
			}
		}
	}
}

// processWrite handles a single write request
func (c *Client) processWrite(req WriteRequest) {
	var err error

	switch req.Type {
	case WriteTypeTaskExecution:
		if task, ok := req.Data.(*TaskExecution); ok {
			err = c.SaveTaskExecution(context.Background(), task)
		}
	case WriteTypeAgentExecution:
		if agent, ok := req.Data.(*AgentExecution); ok {
			err = c.SaveAgentExecution(context.Background(), agent)
		}
	case WriteTypeToolExecution:
		if tool, ok := req.Data.(*ToolExecution); ok {
			err = c.SaveToolExecution(context.Background(), tool)
		}
	case WriteTypeSessionArchive:
		if session, ok := req.Data.(*SessionArchive); ok {
			err = c.SaveSessionArchive(context.Background(), session)
		}
	case WriteTypeAuditLog:
		if audit, ok := req.Data.(*AuditLog); ok {
			err = c.SaveAuditLog(context.Background(), audit)
		}
	}

	// Call callback if provided
	if req.Callback != nil {
		req.Callback(err)
	}

	if err != nil {
		c.logger.Error("Failed to process write request",
			zap.Int("type", int(req.Type)),
			zap.Error(err),
		)
	}
}

// processBatch handles batch writes
func (c *Client) processBatch(batch []WriteRequest) {
	if len(batch) == 0 {
		return
	}

	c.logger.Debug("Processing batch writes", zap.Int("count", len(batch)))

	// Group by type for efficient batch inserts
	taskExecutions := make([]*TaskExecution, 0)
	agentExecutions := make([]*AgentExecution, 0)
	toolExecutions := make([]*ToolExecution, 0)

	for _, req := range batch {
		switch req.Type {
		case WriteTypeTaskExecution:
			if task, ok := req.Data.(*TaskExecution); ok {
				taskExecutions = append(taskExecutions, task)
			}
		case WriteTypeAgentExecution:
			if agent, ok := req.Data.(*AgentExecution); ok {
				agentExecutions = append(agentExecutions, agent)
			}
		case WriteTypeToolExecution:
			if tool, ok := req.Data.(*ToolExecution); ok {
				toolExecutions = append(toolExecutions, tool)
			}
		case WriteTypeBatch:
			// WriteTypeBatch should contain a slice of inner requests
			if innerReqs, ok := req.Data.([]WriteRequest); ok {
				// Recursively process the inner requests
				for _, innerReq := range innerReqs {
					switch innerReq.Type {
					case WriteTypeTaskExecution:
						if task, ok := innerReq.Data.(*TaskExecution); ok {
							taskExecutions = append(taskExecutions, task)
						}
					case WriteTypeAgentExecution:
						if agent, ok := innerReq.Data.(*AgentExecution); ok {
							agentExecutions = append(agentExecutions, agent)
						}
					case WriteTypeToolExecution:
						if tool, ok := innerReq.Data.(*ToolExecution); ok {
							toolExecutions = append(toolExecutions, tool)
						}
					}
				}
			}
		}
	}

	// Batch insert each type
	ctx := context.Background()

	if len(taskExecutions) > 0 {
		if err := c.BatchSaveTaskExecutions(ctx, taskExecutions); err != nil {
			c.logger.Error("Failed to batch save task executions", zap.Error(err))
		}
	}

	if len(agentExecutions) > 0 {
		if err := c.BatchSaveAgentExecutions(ctx, agentExecutions); err != nil {
			c.logger.Error("Failed to batch save agent executions", zap.Error(err))
		}
	}

	if len(toolExecutions) > 0 {
		if err := c.BatchSaveToolExecutions(ctx, toolExecutions); err != nil {
			c.logger.Error("Failed to batch save tool executions", zap.Error(err))
		}
	}
}

// drainQueue processes remaining requests during shutdown
func (c *Client) drainQueue(batchBuffer []WriteRequest) {
	timeout := time.After(10 * time.Second)

	for {
		select {
		case req := <-c.writeQueue:
			c.processWrite(req)
		case <-timeout:
			c.logger.Warn("Timeout draining write queue")
			return
		default:
			// Queue is empty
			if len(batchBuffer) > 0 {
				c.processBatch(batchBuffer)
			}
			return
		}
	}
}

// QueueWrite adds a write request to the async queue
func (c *Client) QueueWrite(writeType WriteType, data interface{}, callback func(error)) error {
	select {
	case c.writeQueue <- WriteRequest{
		Type:     writeType,
		Data:     data,
		Callback: callback,
	}:
		return nil
	default:
		// Queue is full - use synchronous fallback to avoid dropping writes
		c.logger.Warn("Write queue is full, falling back to synchronous write",
			zap.String("type", writeType.String()))

		req := WriteRequest{
			Type:     writeType,
			Data:     data,
			Callback: callback,
		}

		// Execute synchronously
		c.processWrite(req)
		return nil
	}
}

// QueueWriteWithRetry attempts to queue a write with limited retries before fallback
func (c *Client) QueueWriteWithRetry(writeType WriteType, data interface{}, callback func(error)) error {
	const maxRetries = 3
	const retryDelay = 10 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case c.writeQueue <- WriteRequest{
			Type:     writeType,
			Data:     data,
			Callback: callback,
		}:
			return nil
		default:
			if attempt < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			// Final attempt failed, fall back to sync
			c.logger.Warn("Write queue full after retries, using synchronous fallback",
				zap.String("type", writeType.String()),
				zap.Int("attempts", maxRetries))

			req := WriteRequest{
				Type:     writeType,
				Data:     data,
				Callback: callback,
			}
			c.processWrite(req)
			return nil
		}
	}
	return nil
}

// healthCheck periodically checks database connectivity
func (c *Client) healthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := c.db.PingContext(ctx); err != nil {
				c.logger.Error("Database health check failed", zap.Error(err))
			}
			cancel()
		}
	}
}

// Close gracefully shuts down the database client
func (c *Client) Close() error {
	c.logger.Info("Shutting down database client")

	// Signal workers to stop
	close(c.stopCh)

	// Wait for all workers to finish draining
	c.logger.Info("Waiting for write workers to finish")
	c.workerWg.Wait()

	// Close database connection
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	c.logger.Info("Database client closed")
	return nil
}

// GetDB returns the underlying database connection for direct queries
func (c *Client) GetDB() *sql.DB {
	return c.db.GetDB()
}

// Transaction helper for transactional operations using circuit breaker protected transaction
func (c *Client) WithTransactionCB(ctx context.Context, fn func(*circuitbreaker.TxWrapper) error) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %v, original error: %w", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	return nil
}

// Transaction helper for transactional operations (legacy API, bypasses circuit breaker)
// Deprecated: Use WithTransactionCB for circuit breaker protection
func (c *Client) WithTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
	rawTx, err := c.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			rawTx.Rollback()
			panic(p)
		}
	}()

	if err := fn(rawTx); err != nil {
		if rbErr := rawTx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %v, original error: %w", rbErr, err)
		}
		return err
	}

	if err := rawTx.Commit(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	return nil
}

// Wrapper returns the underlying DatabaseWrapper for health checks and monitoring
func (c *Client) Wrapper() *circuitbreaker.DatabaseWrapper {
	return c.db
}
