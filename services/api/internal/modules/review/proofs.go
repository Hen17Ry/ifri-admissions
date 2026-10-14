package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/platform/storage"
)

// ProofStream is separate from the analysis stream: the worker serves it from its own loop,
// so a proof never waits behind candidates' OCR analyses.
const ProofStream = "ifri:ocr:proofs:v1"

const (
	maxProofBytes   = 2 << 20
	maxProofSide    = 4000
	maxProofRetries = 3
)

type ProofStatus struct {
	Status      string `json:"status"`
	FailureCode string `json:"failureCode,omitempty"`
}

// extractedSubject resolves the OCR row behind one subject of a visible dossier.
func (s *Service) extractedSubject(ctx context.Context, tx pgx.Tx, reader admin.Principal, id, subject string) (string, error) {
	if err := s.visible(ctx, tx, reader, id); err != nil {
		return "", err
	}
	if !validUUID(subject) {
		return "", ErrNotFound
	}
	var extracted string
	err := tx.QueryRow(ctx, `SELECT x.ocr_extracted_subject_id::text FROM application_submission_subjects x JOIN application_submissions s ON s.id=x.submission_id
	 WHERE s.application_id=$1::uuid AND x.subject_id=$2::uuid`, id, subject).Scan(&extracted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", admin.NewProblem(404, "SUBJECT_NOT_FOUND", "Cette matière n’appartient pas au dossier.")
	}
	return extracted, err
}

func (s *Service) proofStatus(ctx context.Context, q pgx.Tx, extracted string) (ProofStatus, string, error) {
	var p ProofStatus
	var key *string
	var failure *string
	err := q.QueryRow(ctx, `SELECT status,failure_code,storage_key FROM ocr_proof_images WHERE ocr_extracted_subject_id=$1::uuid`, extracted).Scan(&p.Status, &failure, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProofStatus{Status: "NONE"}, "", nil
	}
	if failure != nil {
		p.FailureCode = *failure
	}
	if key != nil {
		return p, *key, err
	}
	return p, "", err
}

// RequestProof creates (or retries) the generation of a proof image and queues it at once.
func (s *Service) RequestProof(ctx context.Context, reader admin.Principal, id, subject string) (ProofStatus, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ProofStatus{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	extracted, err := s.extractedSubject(ctx, tx, reader, id, subject)
	if err != nil {
		return ProofStatus{}, err
	}
	var proof, status string
	err = tx.QueryRow(ctx, `INSERT INTO ocr_proof_images(ocr_extracted_subject_id,application_id) VALUES($1::uuid,$2::uuid)
	 ON CONFLICT (ocr_extracted_subject_id) DO UPDATE SET status='PENDING',failure_code=NULL,attempts=ocr_proof_images.attempts+1,requested_at=now(),dispatched_at=NULL
	  WHERE ocr_proof_images.status='FAILED' AND ocr_proof_images.attempts<$3
	 RETURNING id::text,status`, extracted, id, maxProofRetries).Scan(&proof, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		current, _, err := s.proofStatus(ctx, tx, extracted)
		return current, err
	}
	if err != nil {
		return ProofStatus{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ProofStatus{}, err
	}
	if err = s.dispatch(ctx, proof); err != nil {
		s.Logger.Error("proof dispatch failed; the background loop will retry", "proof_id", proof)
	}
	return ProofStatus{Status: status}, nil
}

func (s *Service) ProofState(ctx context.Context, reader admin.Principal, id, subject string) (ProofStatus, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ProofStatus{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	extracted, err := s.extractedSubject(ctx, tx, reader, id, subject)
	if err != nil {
		return ProofStatus{}, err
	}
	p, _, err := s.proofStatus(ctx, tx, extracted)
	return p, err
}

// ProofImage streams a ready proof and traces the consultation.
func (s *Service) ProofImage(ctx context.Context, reader admin.Principal, id, subject string) (storage.Object, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return storage.Object{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	extracted, err := s.extractedSubject(ctx, tx, reader, id, subject)
	if err != nil {
		return storage.Object{}, err
	}
	p, key, err := s.proofStatus(ctx, tx, extracted)
	if err != nil {
		return storage.Object{}, err
	}
	if p.Status != "READY" {
		return storage.Object{}, ErrProofNotReady
	}
	if err = audit.RecordAccess(ctx, tx, reader.UserID, "APPLICATION", id, "OCR_PROOF:"+subject); err != nil {
		return storage.Object{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return storage.Object{}, err
	}
	obj, err := s.Objects.Get(ctx, key)
	if err != nil || obj.ContentType != "image/png" {
		if err == nil {
			obj.Body.Close()
		}
		return storage.Object{}, ErrStorage
	}
	return obj, nil
}

// dispatch sends one proof job built from the exact analysed document version and the
// coordinates stored with that analysis.
func (s *Service) dispatch(ctx context.Context, proof string) error {
	var job proofJob
	job.ContractVersion = 1
	job.ProofID = proof
	err := s.Pool.QueryRow(ctx, `SELECT x.ocr_analysis_id::text,o.document_version_id::text,v.sha256,x.page_number,x.evidence
	 FROM ocr_proof_images p JOIN ocr_extracted_subjects x ON x.id=p.ocr_extracted_subject_id JOIN ocr_analyses o ON o.id=x.ocr_analysis_id
	 JOIN document_versions v ON v.id=o.document_version_id WHERE p.id=$1::uuid AND p.status='PENDING'`, proof).
		Scan(&job.AnalysisID, &job.DocumentVersion, &job.DocumentSHA256, &job.Page, &job.Evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err = s.Cache.XAdd(ctx, &redis.XAddArgs{Stream: s.stream(), MaxLen: 10000, Approx: true, Values: map[string]any{"job": string(body)}}).Err(); err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE ocr_proof_images SET dispatched_at=now() WHERE id=$1::uuid AND status='PENDING'`, proof)
	return err
}

// DispatchProofs re-sends proof jobs lost in transit and fails those that never succeed.
func (s *Service) DispatchProofs(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.redispatch(ctx); err != nil {
				s.Logger.Error("proof dispatch unavailable")
			}
		}
	}
}

func (s *Service) redispatch(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `UPDATE ocr_proof_images SET status='FAILED',failure_code='RENDER_FAILED',completed_at=now()
	 WHERE status='PENDING' AND requested_at<now()-interval '10 minutes'`); err != nil {
		return err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id::text FROM ocr_proof_images WHERE status='PENDING' AND (dispatched_at IS NULL OR dispatched_at<now()-interval '2 minutes') ORDER BY requested_at LIMIT 20`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.dispatch(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// RegisterInternal mounts the worker's proof routes on the private listener (bearer secret).
func (s *Service) RegisterInternal(m *http.ServeMux, secret string) {
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
				w.WriteHeader(401)
				return
			}
			next(w, r)
		}
	}
	m.HandleFunc("GET /internal/proofs/{id}/document", auth(s.proofDocument))
	m.HandleFunc("POST /internal/proofs/{id}/image", auth(s.proofUpload))
	m.HandleFunc("POST /internal/proofs/{id}/failure", auth(s.proofFailure))
}

func (s *Service) proofDocument(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var key string
	if !validUUID(r.PathValue("id")) || s.Pool.QueryRow(ctx, `SELECT v.storage_key FROM ocr_proof_images p JOIN ocr_extracted_subjects x ON x.id=p.ocr_extracted_subject_id
	 JOIN ocr_analyses o ON o.id=x.ocr_analysis_id JOIN document_versions v ON v.id=o.document_version_id
	 WHERE p.id=$1::uuid AND p.status='PENDING'`, r.PathValue("id")).Scan(&key) != nil {
		w.WriteHeader(404)
		return
	}
	obj, err := s.Objects.Get(ctx, key)
	if err != nil {
		w.WriteHeader(404)
		return
	}
	defer obj.Body.Close()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, obj.Body)
}

// proofUpload accepts only a bounded PNG for a pending proof, stores it privately, then READY.
func (s *Service) proofUpload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id := r.PathValue("id")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxProofBytes))
	if err != nil || !validUUID(id) || r.Header.Get("Content-Type") != "image/png" {
		w.WriteHeader(400)
		return
	}
	config, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxProofSide || config.Height > maxProofSide {
		_, _ = s.Pool.Exec(ctx, `UPDATE ocr_proof_images SET status='FAILED',failure_code='IMAGE_REJECTED',completed_at=now() WHERE id=$1::uuid AND status='PENDING'`, id)
		w.WriteHeader(422)
		return
	}
	var app string
	if err = s.Pool.QueryRow(ctx, `SELECT application_id::text FROM ocr_proof_images WHERE id=$1::uuid AND status='PENDING'`, id).Scan(&app); err != nil {
		w.WriteHeader(409)
		return
	}
	sum := sha256.Sum256(body)
	key := fmt.Sprintf("applications/%s/proofs/%s.png", app, id)
	if err = s.Objects.PutContent(ctx, key, bytes.NewReader(body), int64(len(body)), "image/png"); err != nil {
		w.WriteHeader(503)
		return
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE ocr_proof_images SET status='READY',storage_key=$2,sha256=$3,size_bytes=$4,width=$5,height=$6,completed_at=now()
	 WHERE id=$1::uuid AND status='PENDING'`, id, key, hex.EncodeToString(sum[:]), len(body), config.Width, config.Height); err != nil {
		w.WriteHeader(503)
		return
	}
	s.Logger.Info("OCR proof stored", "proof_id", id, "size_bytes", len(body))
	w.WriteHeader(204)
}

func (s *Service) proofFailure(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"failureCode"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	dec.DisallowUnknownFields()
	codes := map[string]bool{"DOCUMENT_UNAVAILABLE": true, "PAGE_MISSING": true, "RENDER_FAILED": true}
	if dec.Decode(&input) != nil || !codes[input.Code] || !validUUID(r.PathValue("id")) {
		w.WriteHeader(400)
		return
	}
	if _, err := s.Pool.Exec(r.Context(), `UPDATE ocr_proof_images SET status='FAILED',failure_code=$2,completed_at=now() WHERE id=$1::uuid AND status='PENDING'`, r.PathValue("id"), input.Code); err != nil {
		w.WriteHeader(503)
		return
	}
	s.Logger.Info("OCR proof failed", "proof_id", r.PathValue("id"), "failure_code", input.Code)
	w.WriteHeader(204)
}
