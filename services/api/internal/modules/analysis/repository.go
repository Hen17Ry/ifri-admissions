package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/modules/references"
	"strings"
	"time"
)

type Repository struct {
	Pool      *pgxpool.Pool
	Threshold float64
}
type ExtractionView struct {
	Extraction
	Code string `json:"code"`
	Name string `json:"name"`
}
type View struct {
	ApplicationVersion int64            `json:"applicationVersion"`
	ID                 string           `json:"id"`
	DocumentVersion    string           `json:"documentVersionId"`
	Status             string           `json:"status"`
	Confirmation       string           `json:"candidateConfirmation"`
	Failure            string           `json:"failureCode,omitempty"`
	Subjects           []ExtractionView `json:"subjects"`
	Ready              bool             `json:"verificationReady"`
}

const currentSQL = `SELECT o.id::text,o.document_version_id::text,o.status,o.candidate_confirmation,coalesce(o.failure_code,''),a.version FROM ocr_analyses o JOIN applications a ON a.id=o.application_id JOIN application_documents d ON d.application_id=a.id AND d.document_type='TRANSCRIPT' JOIN campaign_programs cp ON cp.id=a.campaign_program_id JOIN rule_set_versions g ON g.id=o.rule_grid_version_id AND g.campaign_program_id=cp.id AND g.status='PUBLISHED' WHERE a.id=$1::uuid AND o.document_version_id=d.current_version_id AND o.origin_qualification_id=a.origin_qualification_id AND o.program_id=cp.program_id AND o.status<>'OBSOLETE' AND cp.enabled AND EXISTS (SELECT 1 FROM programs p WHERE p.id=cp.program_id AND p.enabled) AND EXISTS (SELECT 1 FROM origin_qualifications q WHERE q.id=a.origin_qualification_id AND q.enabled) AND EXISTS (SELECT 1 FROM program_qualification_eligibilities e WHERE e.rule_set_version_id=g.id AND e.origin_qualification_id=a.origin_qualification_id AND e.eligible) AND NOT EXISTS (SELECT 1 FROM expected_subject_rules r JOIN subjects s ON s.id=r.subject_id JOIN program_qualification_eligibilities e ON e.id=r.eligibility_id WHERE e.rule_set_version_id=g.id AND e.origin_qualification_id=a.origin_qualification_id AND NOT s.enabled) ORDER BY o.created_at DESC LIMIT 1`

func (r Repository) Current(ctx context.Context, hash []byte) (View, error) {
	d, err := drafts.Read(ctx, r.Pool, hash, false)
	if err != nil {
		return View{}, err
	}
	return r.view(ctx, r.Pool, d.ID)
}
func (r Repository) view(ctx context.Context, q references.Queryer, app string) (View, error) {
	v := View{Subjects: []ExtractionView{}}
	err := q.QueryRow(ctx, currentSQL, app).Scan(&v.ID, &v.DocumentVersion, &v.Status, &v.Confirmation, &v.Failure, &v.ApplicationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		var rejected bool
		if checkErr := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM application_documents d JOIN ocr_analyses o ON o.document_version_id=d.current_version_id AND o.application_id=d.application_id WHERE d.application_id=$1::uuid AND d.document_type='TRANSCRIPT' AND o.candidate_confirmation='REJECTED')`, app).Scan(&rejected); checkErr != nil {
			return v, checkErr
		}
		if rejected {
			return v, &drafts.Problem{Status: 409, Code: "TRANSCRIPT_REPLACEMENT_REQUIRED", Message: "Ce relevé a été refusé. Fournissez une nouvelle version pour relancer son analyse."}
		}

		return v, &drafts.Problem{Status: 409, Code: "ANALYSIS_NOT_AVAILABLE", Message: "Choisissez votre filière et ajoutez un nouveau relevé pour lancer son analyse."}
	}
	if err != nil {
		return v, err
	}
	rows, err := q.Query(ctx, `SELECT x.subject_id::text,x.position,x.detected_subject_text,x.grade_raw,x.grade_value::text,x.coefficient_raw,x.coefficient_value::text,x.page_number,x.subject_confidence,x.grade_confidence,x.coefficient_confidence,x.overall_confidence,x.evidence,s.code,s.name FROM ocr_extracted_subjects x JOIN subjects s ON s.id=x.subject_id WHERE x.ocr_analysis_id=$1::uuid ORDER BY x.position`, v.ID)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var s ExtractionView
		var b []byte
		if err = rows.Scan(&s.SubjectID, &s.Position, &s.Detected, &s.GradeRaw, &s.Grade, &s.CoefficientRaw, &s.Coefficient, &s.Page, &s.SubjectConfidence, &s.GradeConfidence, &s.CoefficientConfidence, &s.Confidence, &b, &s.Code, &s.Name); err != nil {
			return v, err
		}
		if err = json.Unmarshal(b, &s.Evidence); err != nil {
			return v, err
		}
		v.Subjects = append(v.Subjects, s)
	}
	if err = rows.Err(); err != nil {
		return v, err
	}
	var docs bool
	err = q.QueryRow(ctx, `SELECT count(*)=4 FROM application_documents WHERE application_id=$1::uuid`, app).Scan(&docs)
	v.Ready = docs && v.Status == "SUCCEEDED" && v.Confirmation == "CONFIRMED"
	return v, err
}
func (r Repository) Decide(ctx context.Context, hash []byte, id string, version int64, decision string) (View, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(ctx)
	d, err := drafts.Read(ctx, tx, hash, true)
	if err != nil {
		return View{}, err
	}
	if d.Version != version {
		return View{}, drafts.ErrConflict
	}
	if d.State != "DRAFT" || d.Campaign.Status != "OPEN" || time.Now().Before(d.Campaign.StartsAt) || !time.Now().Before(d.Campaign.EndsAt) {
		return View{}, drafts.ErrClosed
	}
	v, err := r.view(ctx, tx, d.ID)
	if err != nil {
		return v, err
	}
	if v.ID != id || v.Status != "SUCCEEDED" || v.Confirmation != "PENDING" {
		return v, &drafts.Problem{Status: 409, Code: "ANALYSIS_NOT_CONFIRMABLE", Message: "Cette analyse ne peut plus être confirmée. Rechargez le brouillon."}
	}
	_, err = tx.Exec(ctx, `UPDATE ocr_analyses SET candidate_confirmation=$2,confirmed_at=now(),updated_at=now() WHERE id=$1::uuid`, id, decision)
	if err != nil {
		return v, err
	}
	if err = tx.Commit(ctx); err != nil {
		return v, err
	}
	return r.Current(ctx, hash)
}
func (r Repository) Complete(ctx context.Context, id string, result Result) error {
	// Lock application before analysis, matching academic/document updates.
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var app string
	if err = tx.QueryRow(ctx, `SELECT application_id::text FROM ocr_analyses WHERE id=$1::uuid`, id).Scan(&app); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM applications WHERE id=$1::uuid FOR UPDATE`, app); err != nil {
		return err
	}
	var status string
	var expectedJSON []byte
	var digest *string
	var attempt int
	if err = tx.QueryRow(ctx, `SELECT status,expected_subjects,result_digest,attempt_number FROM ocr_analyses WHERE id=$1::uuid FOR UPDATE`, id).Scan(&status, &expectedJSON, &digest, &attempt); err != nil {
		return err
	}
	var expected []Expected
	if err = json.Unmarshal(expectedJSON, &expected); err != nil {
		return err
	}
	if err = Validate(result, expected, r.Threshold); err != nil {
		return err
	}
	sum := sha256.Sum256(jsonBytes(result))
	h := hex.EncodeToString(sum[:])
	var previous string
	priorErr := tx.QueryRow(ctx, `SELECT callback_digest FROM ocr_analysis_attempts WHERE ocr_analysis_id=$1::uuid AND attempt_number=$2`, id, result.Attempt).Scan(&previous)
	if priorErr == nil {
		if previous == h {
			return nil
		}
		return errors.New("conflicting attempt callback")
	}
	if !errors.Is(priorErr, pgx.ErrNoRows) {
		return priorErr
	}
	if digest != nil {
		if *digest == h {
			return nil
		}
		return errors.New("conflicting result")
	}
	if status != "PROCESSING" && status != "OBSOLETE" {
		return errors.New("analysis not processing")
	}
	if result.Attempt != attempt {
		return errors.New("stale attempt")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ocr_analysis_attempts(ocr_analysis_id,attempt_number,callback_digest,reported_status,failure_code,engine,engine_version) VALUES($1::uuid,$2,$3,$4,nullif($5,''),$6,$7)`, id, result.Attempt, h, result.Status, result.Failure, result.Engine, result.EngineVersion); err != nil {
		return err
	}
	if (result.Failure == "OCR_ENGINE_ERROR" || result.Failure == "ANALYSIS_TIMEOUT") && attempt < 3 && status != "OBSOLETE" {
		_, err = tx.Exec(ctx, `UPDATE ocr_analyses SET status='QUEUED',failure_code=$2,dispatched_at=NULL,updated_at=now() WHERE id=$1::uuid`, id, result.Failure)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	for _, s := range result.Subjects {
		_, err = tx.Exec(ctx, `INSERT INTO ocr_extracted_subjects(ocr_analysis_id,subject_id,position,detected_subject_text,grade_raw,grade_value,coefficient_raw,coefficient_value,page_number,subject_confidence,grade_confidence,coefficient_confidence,overall_confidence,evidence) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6::numeric,$7,$8::numeric,$9,$10,$11,$12,$13,$14::jsonb)`, id, s.SubjectID, s.Position, s.Detected, s.GradeRaw, strings.ReplaceAll(s.Grade, ",", "."), s.CoefficientRaw, strings.ReplaceAll(s.Coefficient, ",", "."), s.Page, s.SubjectConfidence, s.GradeConfidence, s.CoefficientConfidence, s.Confidence, jsonBytes(s.Evidence))
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE ocr_analyses SET status=CASE WHEN status='OBSOLETE' THEN status ELSE $2 END,engine=$3,engine_version=$4,failure_code=nullif($5,''),completed_at=now(),updated_at=now(),result_digest=$6 WHERE id=$1::uuid`, id, result.Status, result.Engine, result.EngineVersion, result.Failure, h)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
