package analysis

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"time"
)

const Stream = "ifri:ocr:jobs:v1"

func (r Repository) Dispatch(ctx context.Context, cache *redis.Client, logger *slog.Logger) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := r.dispatchBatch(ctx, cache); err != nil {
				logger.Error("OCR dispatch unavailable")
			}
		}
	}
}
func (r Repository) dispatchBatch(ctx context.Context, cache *redis.Client) error {
	return r.dispatchToStream(ctx, cache, Stream)
}
func (r Repository) dispatchToStream(ctx context.Context, cache *redis.Client, stream string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE ocr_analyses SET status=CASE WHEN attempt_number>=3 THEN 'FAILED' ELSE 'QUEUED' END,failure_code=CASE WHEN attempt_number>=3 THEN 'ANALYSIS_TIMEOUT' ELSE NULL END,dispatched_at=NULL,updated_at=now() WHERE status='PROCESSING' AND started_at<now()-interval '5 minutes'`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT o.id::text,document_version_id::text,rule_grid_version_id::text,expected_subjects,v.sha256 FROM ocr_analyses o JOIN document_versions v ON v.id=o.document_version_id WHERE status='QUEUED' AND (dispatched_at IS NULL OR dispatched_at<now()-interval '5 minutes') ORDER BY o.created_at LIMIT 10 FOR UPDATE OF o SKIP LOCKED`)
	if err != nil {
		return err
	}
	type job struct {
		ContractVersion int             `json:"contractVersion"`
		SHA256          string          `json:"documentSha256"`
		ID              string          `json:"analysisId"`
		DocumentVersion string          `json:"documentVersionId"`
		Grid            string          `json:"gridVersionId"`
		Expected        json.RawMessage `json:"expectedSubjects"`
	}
	jobs := []job{}
	for rows.Next() {
		j := job{ContractVersion: 1}
		if err = rows.Scan(&j.ID, &j.DocumentVersion, &j.Grid, &j.Expected, &j.SHA256); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for _, j := range jobs {
		if err = cache.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"job": string(jsonBytes(j))}}).Err(); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE ocr_analyses SET dispatched_at=now() WHERE id=$1::uuid`, j.ID); err != nil {
			return err
		}
	}
	// XADD may succeed before rollback: duplicates are intentional and callback is idempotent.
	return tx.Commit(ctx)
}
