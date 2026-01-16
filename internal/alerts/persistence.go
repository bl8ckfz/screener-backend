package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const (
	// batchSize is the maximum number of alerts to batch before flushing
	batchSize = 50
	// flushInterval is how often to flush alerts to the database
	flushInterval = 5 * time.Second
)

// AlertPersister handles batched writing of alerts to TimescaleDB
type AlertPersister struct {
	db     *pgxpool.Pool
	logger zerolog.Logger
	queue  []*Alert
	mu     sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
	wg     sync.WaitGroup
}

// NewAlertPersister creates a new alert persister
func NewAlertPersister(db *pgxpool.Pool, logger zerolog.Logger) *AlertPersister {
	p := &AlertPersister{
		db:     db,
		logger: logger,
		queue:  make([]*Alert, 0, batchSize),
		ticker: time.NewTicker(flushInterval),
		done:   make(chan struct{}),
	}

	// Start background flusher
	p.wg.Add(1)
	go p.flusher()

	return p
}

// SaveAlert adds an alert to the batch queue
func (p *AlertPersister) SaveAlert(alert *Alert) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.queue = append(p.queue, alert)

	// Flush if batch is full
	if len(p.queue) >= batchSize {
		p.flushLocked()
	}
}

// flusher runs in the background and periodically flushes the queue
func (p *AlertPersister) flusher() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ticker.C:
			p.mu.Lock()
			if len(p.queue) > 0 {
				p.flushLocked()
			}
			p.mu.Unlock()

		case <-p.done:
			p.mu.Lock()
			if len(p.queue) > 0 {
				p.flushLocked()
			}
			p.mu.Unlock()
			return
		}
	}
}

// flushLocked flushes the current batch to the database (must hold mutex)
func (p *AlertPersister) flushLocked() {
	if len(p.queue) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Copy queue and reset
	alerts := make([]*Alert, len(p.queue))
	copy(alerts, p.queue)
	p.queue = p.queue[:0]

	// Write to database
	if err := p.writeAlerts(ctx, alerts); err != nil {
		p.logger.Error().Err(err).Int("count", len(alerts)).Msg("Failed to persist alerts")
		return
	}

	p.logger.Debug().Int("count", len(alerts)).Msg("Persisted alerts to database")
}

// writeAlerts writes a batch of alerts to TimescaleDB
func (p *AlertPersister) writeAlerts(ctx context.Context, alerts []*Alert) error {
	query := `
		INSERT INTO alert_history (
			time,
			symbol,
			rule_type,
			description,
			price,
			metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
	`

	batch := &pgxBatch{
		ctx: ctx,
		db:  p.db,
	}

	for _, alert := range alerts {
		// Convert metadata to JSON
		metadataJSON, err := json.Marshal(alert.Metadata)
		if err != nil {
			p.logger.Error().Err(err).Str("symbol", alert.Symbol).Msg("Failed to marshal metadata")
			continue
		}

		batch.Queue(query,
			alert.Timestamp,
			alert.Symbol,
			alert.RuleType,
			alert.Description,
			alert.Price,
			metadataJSON,
		)
	}

	return batch.SendBatch()
}

// Close stops the persister and flushes remaining alerts
func (p *AlertPersister) Close() error {
	close(p.done)
	p.ticker.Stop()
	p.wg.Wait()
	return nil
}

// pgxBatch is a helper for batch operations
type pgxBatch struct {
	ctx     context.Context
	db      *pgxpool.Pool
	queries []string
	args    [][]interface{}
}

// Queue adds a query to the batch
func (b *pgxBatch) Queue(query string, args ...interface{}) {
	b.queries = append(b.queries, query)
	b.args = append(b.args, args)
}

// SendBatch executes all queued queries in a transaction
func (b *pgxBatch) SendBatch() error {
	if len(b.queries) == 0 {
		return nil
	}

	tx, err := b.db.Begin(b.ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(b.ctx)

	for i, query := range b.queries {
		if _, err := tx.Exec(b.ctx, query, b.args[i]...); err != nil {
			return fmt.Errorf("failed to execute query %d: %w", i, err)
		}
	}

	if err := tx.Commit(b.ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
