package submissions

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ifri-admissions/services/api/internal/modules/analysis"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/modules/references"
)

type Repository struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
}

const receiptSQL = `SELECT a.application_number,a.submitted_at,s.last_name,s.first_names,k.code,k.name,k.academic_year,
 p.code,p.name,q.code,q.label,s.graduation_year
 FROM application_submissions s JOIN applications a ON a.id=s.application_id
 JOIN admission_campaigns k ON k.id=s.campaign_id JOIN programs p ON p.id=s.program_id
 JOIN origin_qualifications q ON q.id=s.origin_qualification_id WHERE s.application_id=$1::uuid`

func readReceipt(ctx context.Context, q references.Queryer, application string, expires time.Time) (Receipt, error) {
	var r Receipt
	err := q.QueryRow(ctx, receiptSQL, application).Scan(&r.ApplicationNumber, &r.SubmittedAt, &r.Candidate.LastName, &r.Candidate.FirstNames, &r.Campaign.Code, &r.Campaign.Name, &r.Campaign.AcademicYear, &r.Program.Code, &r.Program.Name, &r.Qualification.Code, &r.Qualification.Name, &r.GraduationYear)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotSubmitted
	}
	r.SessionExpiresAt = expires
	return r, err
}

// Receipt is one of the two reads a session keeps after submission (with GET /current).
func (r *Repository) Receipt(ctx context.Context, hash []byte) (Receipt, error) {
	d, err := drafts.Read(ctx, r.Pool, hash, false)
	if err != nil {
		return Receipt{}, err
	}
	if d.State != "SUBMITTED" {
		return Receipt{}, ErrNotSubmitted
	}
	return readReceipt(ctx, r.Pool, d.ID, d.SessionExpiresAt)
}

type document struct{ Type, VersionID, SHA256 string }
type expected struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
}
type current struct {
	ID, DocumentVersion, Grid, Program, Qualification, Status, Confirmation string
	Expected                                                                []expected
}
type subject struct {
	ExtractedID, SubjectID, Code, Name, GradeRaw, Grade, CoefficientRaw, Coefficient, Method string
	Position                                                                                 int
	Reconstructed                                                                            []string
	Evidence                                                                                 analysis.Evidence
}
type flag struct{ Code, Related string }

// Submit runs the whole submission in one transaction: lock, revalidation, exact average,
// number, snapshot, flags, audit and the SUBMITTED transition. Nothing is written unless all
// of it commits, except the ADMISSION_RULES_CHANGED invalidation, committed on purpose.
func (r *Repository) Submit(ctx context.Context, hash []byte, version int64) (Receipt, Outcome, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return Receipt{}, Outcome{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	// Concurrent submissions of one application queue on this row lock; the later one reads
	// SUBMITTED and replays the same receipt.
	d, err := drafts.Read(ctx, tx, hash, true)
	if err != nil {
		return Receipt{}, Outcome{}, err
	}
	out := Outcome{ApplicationID: d.ID}
	if d.State == "SUBMITTED" {
		out.Replay = true
		if err = tx.QueryRow(ctx, `SELECT id::text FROM application_submissions WHERE application_id=$1::uuid`, d.ID).Scan(&out.SubmissionID); err != nil {
			return Receipt{}, out, err
		}
		receipt, err := readReceipt(ctx, tx, d.ID, d.SessionExpiresAt)
		return receipt, out, err
	}
	if err = drafts.Editable(d); err != nil {
		return Receipt{}, out, err
	}
	if d.Version != version {
		return Receipt{}, out, ErrVersionConflict
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM admission_campaigns WHERE id=$1::uuid FOR SHARE`, d.Campaign.ID); err != nil {
		return Receipt{}, out, err
	}
	rules := references.NewService(references.NewRepository(tx))
	if _, err = rules.Campaign(ctx, d.Campaign.Code); err != nil {
		if errors.Is(err, references.ErrCampaignNotOpen) || errors.Is(err, references.ErrCampaignNotFound) {
			return Receipt{}, out, ErrCampaignClosed
		}
		return Receipt{}, out, err
	}
	docs, err := currentDocuments(ctx, tx, d.ID)
	if err != nil {
		return Receipt{}, out, err
	}
	if fields := incomplete(d, docs); len(fields) > 0 {
		return Receipt{}, out, &drafts.Problem{Status: 422, Code: "APPLICATION_INCOMPLETE", Message: "Votre dossier est incomplet : complétez les éléments signalés avant de soumettre.", Fields: fields}
	}
	resolved, err := rules.Resolve(ctx, d.Campaign.Code, d.Program.Code, d.Qualification.Code)
	switch {
	case errors.Is(err, references.ErrIncompleteRules):
		return Receipt{}, out, r.rulesChanged(ctx, tx, d.ID, "")
	case errors.Is(err, references.ErrProgramNotFound), errors.Is(err, references.ErrQualificationNotFound):
		return Receipt{}, out, drafts.ErrIneligible
	case err != nil:
		return Receipt{}, out, err
	case !resolved.Eligible:
		return Receipt{}, out, drafts.ErrIneligible
	}
	var candidate, campaignProgram, program, qualification, grid string
	var year int
	if err = tx.QueryRow(ctx, `SELECT a.candidate_id::text,a.campaign_program_id::text,cp.program_id::text,a.origin_qualification_id::text,v.id::text,k.numbering_year
	 FROM applications a JOIN campaign_programs cp ON cp.id=a.campaign_program_id
	 JOIN rule_set_versions v ON v.campaign_program_id=cp.id AND v.status='PUBLISHED'
	 JOIN admission_campaigns k ON k.id=a.campaign_id WHERE a.id=$1::uuid`, d.ID).Scan(&candidate, &campaignProgram, &program, &qualification, &grid, &year); err != nil {
		return Receipt{}, out, err
	}
	want, err := expectedSubjects(ctx, tx, grid, qualification)
	if err != nil {
		return Receipt{}, out, err
	}
	cur, err := currentAnalysis(ctx, tx, d.ID)
	if err != nil {
		return Receipt{}, out, err
	}
	if cur != nil && (cur.Grid != grid || !sameSubjects(cur.Expected, want)) {
		return Receipt{}, out, r.rulesChanged(ctx, tx, d.ID, cur.ID)
	}
	if cur == nil || cur.Status != "SUCCEEDED" || cur.Confirmation != "CONFIRMED" || cur.DocumentVersion != docs["TRANSCRIPT"].VersionID || cur.Program != program || cur.Qualification != qualification {
		var confirmedBefore bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ocr_analyses WHERE application_id=$1::uuid AND candidate_confirmation='CONFIRMED')`, d.ID).Scan(&confirmedBefore); err != nil {
			return Receipt{}, out, err
		}
		if confirmedBefore {
			return Receipt{}, out, ErrAnalysisOutdated
		}
		return Receipt{}, out, ErrAnalysisRequired
	}
	subjects, grades, err := extracted(ctx, tx, cur.ID, want)
	if errors.Is(err, errSubjectsMismatch) {
		r.Logger.Warn("submission refused: confirmed analysis inconsistent", "application_id", d.ID, "analysis_id", cur.ID)
		return Receipt{}, out, ErrAnalysisRequired
	}
	if err != nil {
		return Receipt{}, out, err
	}
	average, err := ComputeAverage(grades)
	if err != nil {
		return Receipt{}, out, ErrAnalysisRequired
	}
	weighted, err := ExactDecimal(average.WeightedSum)
	if err != nil {
		return Receipt{}, out, err
	}
	coefficients, err := ExactDecimal(average.CoefficientSum)
	if err != nil {
		return Receipt{}, out, err
	}
	// Serialize the submissions of one campaign: concurrent duplicates always see each other.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ifri-submission:' || $1::text, 0))`, d.Campaign.ID); err != nil {
		return Receipt{}, out, err
	}
	transcript := docs["TRANSCRIPT"]
	flags, err := duplicates(ctx, tx, d, transcript.SHA256)
	if err != nil {
		return Receipt{}, out, err
	}
	if analysis.DeclaredSeries(d.Qualification.Code) != "" && !seriesRead(subjects) {
		flags = append(flags, flag{Code: "SERIES_UNREADABLE"})
	}
	var serial int
	err = tx.QueryRow(ctx, `INSERT INTO application_number_counters(numbering_year,program_id,last_serial) VALUES($1,$2::uuid,1)
	 ON CONFLICT (numbering_year,program_id) DO UPDATE SET last_serial=application_number_counters.last_serial+1 RETURNING last_serial`, year, program).Scan(&serial)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "application_number_counters_last_serial_check" {
		return Receipt{}, out, ErrNumbersExhausted
	}
	if err != nil {
		return Receipt{}, out, err
	}
	number, err := FormatNumber(year, d.Program.Code, serial)
	if err != nil {
		return Receipt{}, out, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO application_submissions(application_id,campaign_id,campaign_program_id,program_id,origin_qualification_id,graduation_year,
	 last_name,first_names,email,phone,validation_path,rule_set_version_id,ocr_analysis_id,transcript_version_id,transcript_sha256,
	 weighted_sum,coefficient_sum,average,average_is_exact,calculation_version)
	 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,$10,'CANDIDATE_CONFIRMED_OCR',$11::uuid,$12::uuid,$13::uuid,$14,$15::numeric,$16::numeric,$17::numeric,$18,$19)
	 RETURNING id::text`, d.ID, d.Campaign.ID, campaignProgram, program, qualification, *d.GraduationYear,
		d.Profile.LastName, d.Profile.FirstNames, d.Profile.Email, d.Profile.Phone, grid, cur.ID, transcript.VersionID, transcript.SHA256,
		weighted, coefficients, average.Decimal, average.Exact, CalculationVersion).Scan(&out.SubmissionID); err != nil {
		return Receipt{}, out, err
	}
	for _, s := range subjects {
		if _, err = tx.Exec(ctx, `INSERT INTO application_submission_subjects(submission_id,ocr_extracted_subject_id,subject_id,subject_code,subject_name,position,
		 grade_raw,grade_value,coefficient_raw,coefficient_value,validation_method,reconstructed_cells)
		 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8::numeric,$9,$10::numeric,$11,$12)`, out.SubmissionID, s.ExtractedID, s.SubjectID, s.Code, s.Name, s.Position,
			s.GradeRaw, s.Grade, s.CoefficientRaw, s.Coefficient, s.Method, s.Reconstructed); err != nil {
			return Receipt{}, out, err
		}
	}
	for _, t := range documentTypes {
		if _, err = tx.Exec(ctx, `INSERT INTO application_submission_documents(submission_id,document_type,document_version_id,sha256) VALUES($1::uuid,$2,$3::uuid,$4)`, out.SubmissionID, t, docs[t].VersionID, docs[t].SHA256); err != nil {
			return Receipt{}, out, err
		}
	}
	codes := []string{}
	for _, f := range flags {
		if _, err = tx.Exec(ctx, `INSERT INTO application_submission_flags(application_id,code,related_application_id) VALUES($1::uuid,$2,nullif($3,'')::uuid)`, d.ID, f.Code, f.Related); err != nil {
			return Receipt{}, out, err
		}
		codes = append(codes, f.Code)
	}
	if err = audit.Record(ctx, tx, "APPLICATION_SUBMITTED", audit.Actor{Type: audit.ActorCandidate, ID: candidate}, "APPLICATION", d.ID,
		map[string]any{"submissionId": out.SubmissionID, "ocrAnalysisId": cur.ID, "ruleSetVersionId": grid, "transcriptVersionId": transcript.VersionID, "validationPath": "CANDIDATE_CONFIRMED_OCR", "calculationVersion": CalculationVersion, "subjectCount": len(subjects), "flags": codes}); err != nil {
		return Receipt{}, out, err
	}
	tag, err := tx.Exec(ctx, `UPDATE applications SET state='SUBMITTED',application_number=$2,submitted_at=now(),version=version+1,updated_at=now() WHERE id=$1::uuid AND state='DRAFT' AND version=$3`, d.ID, number, version)
	if err != nil {
		return Receipt{}, out, err
	}
	if tag.RowsAffected() != 1 {
		return Receipt{}, out, ErrVersionConflict
	}
	// Read-only access to the receipt until the campaign ends, and at least a week.
	var expires time.Time
	if err = tx.QueryRow(ctx, `UPDATE application_draft_sessions s SET expires_at=greatest(s.expires_at,k.ends_at,now()+interval '7 days')
	 FROM applications a JOIN admission_campaigns k ON k.id=a.campaign_id WHERE s.secret_hash=$1 AND a.id=s.application_id RETURNING s.expires_at`, hash).Scan(&expires); err != nil {
		return Receipt{}, out, err
	}
	receipt, err := readReceipt(ctx, tx, d.ID, expires)
	if err != nil {
		return Receipt{}, out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Receipt{}, out, err
	}
	return receipt, out, nil
}

// rulesChanged invalidates the analysis built on outdated rules and queues a new one, then
// commits that invalidation alone: the submission itself is refused.
func (r *Repository) rulesChanged(ctx context.Context, tx pgx.Tx, application, analysisID string) error {
	if analysisID != "" {
		if _, err := tx.Exec(ctx, `UPDATE ocr_analyses SET status='OBSOLETE',updated_at=now() WHERE id=$1::uuid AND status<>'OBSOLETE'`, analysisID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `SELECT refresh_transcript_analysis($1::uuid)`, application); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	r.Logger.Info("submission refused: admission rules changed, analysis refreshed", "application_id", application)
	return ErrRulesChanged
}

func currentDocuments(ctx context.Context, tx pgx.Tx, application string) (map[string]document, error) {
	rows, err := tx.Query(ctx, `SELECT d.document_type,v.id::text,v.sha256 FROM application_documents d
	 JOIN document_versions v ON v.id=d.current_version_id AND v.application_document_id=d.id
	 WHERE d.application_id=$1::uuid AND v.replaced_at IS NULL`, application)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	docs := map[string]document{}
	for rows.Next() {
		var doc document
		if err = rows.Scan(&doc.Type, &doc.VersionID, &doc.SHA256); err != nil {
			return nil, err
		}
		docs[doc.Type] = doc
	}
	return docs, rows.Err()
}

// incomplete lists, by field, what the candidate still has to provide.
func incomplete(d drafts.Draft, docs map[string]document) map[string]string {
	fields := map[string]string{}
	if _, err := drafts.NormalizeProfile(d.Profile); err != nil {
		var p *drafts.Problem
		if errors.As(err, &p) {
			for key, message := range p.Fields {
				fields[key] = message
			}
		}
	}
	for key, value := range map[string]string{"lastName": d.Profile.LastName, "firstNames": d.Profile.FirstNames, "email": d.Profile.Email, "phone": d.Profile.Phone} {
		if strings.TrimSpace(value) == "" {
			fields[key] = map[string]string{"lastName": "Renseignez votre nom.", "firstNames": "Renseignez vos prénoms.", "email": "Renseignez votre adresse email.", "phone": "Renseignez votre numéro de téléphone."}[key]
		}
	}
	if d.Qualification == nil {
		fields["qualification"] = "Choisissez votre série ou diplôme d’origine."
	}
	if d.GraduationYear == nil {
		fields["graduationYear"] = "Indiquez votre année d’obtention."
	}
	if d.Program == nil {
		fields["program"] = "Choisissez une filière."
	}
	for _, t := range documentTypes {
		if _, ok := docs[t]; !ok {
			fields["document."+t] = "Ajoutez ce document."
		}
	}
	return fields
}

func expectedSubjects(ctx context.Context, tx pgx.Tx, grid, qualification string) ([]expected, error) {
	rows, err := tx.Query(ctx, `SELECT s.id::text,r.position FROM expected_subject_rules r JOIN subjects s ON s.id=r.subject_id
	 JOIN program_qualification_eligibilities e ON e.id=r.eligibility_id
	 WHERE e.rule_set_version_id=$1::uuid AND e.origin_qualification_id=$2::uuid AND e.eligible ORDER BY r.position`, grid, qualification)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []expected{}
	for rows.Next() {
		var e expected
		if err = rows.Scan(&e.ID, &e.Position); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// currentAnalysis returns the application's only non-obsolete analysis, locked, or nil.
func currentAnalysis(ctx context.Context, tx pgx.Tx, application string) (*current, error) {
	var c current
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT id::text,document_version_id::text,rule_grid_version_id::text,program_id::text,origin_qualification_id::text,status,candidate_confirmation,expected_subjects
	 FROM ocr_analyses WHERE application_id=$1::uuid AND status<>'OBSOLETE' ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, application).Scan(&c.ID, &c.DocumentVersion, &c.Grid, &c.Program, &c.Qualification, &c.Status, &c.Confirmation, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &c.Expected); err != nil {
		return nil, err
	}
	return &c, nil
}

func sameSubjects(a, b []expected) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extracted reads the persisted values (never the analysis view) and checks they are exactly
// the expected subjects with grades in [0, 20] and positive coefficients.
func extracted(ctx context.Context, tx pgx.Tx, analysisID string, want []expected) ([]subject, []Grade, error) {
	rows, err := tx.Query(ctx, `SELECT x.id::text,x.subject_id::text,s.code,s.name,x.position,x.grade_raw,x.grade_value::text,x.coefficient_raw,x.coefficient_value::text,x.evidence
	 FROM ocr_extracted_subjects x JOIN subjects s ON s.id=x.subject_id WHERE x.ocr_analysis_id=$1::uuid ORDER BY x.position`, analysisID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	subjects := []subject{}
	for rows.Next() {
		var s subject
		var raw []byte
		if err = rows.Scan(&s.ExtractedID, &s.SubjectID, &s.Code, &s.Name, &s.Position, &s.GradeRaw, &s.Grade, &s.CoefficientRaw, &s.Coefficient, &raw); err != nil {
			return nil, nil, err
		}
		if err = json.Unmarshal(raw, &s.Evidence); err != nil {
			return nil, nil, errSubjectsMismatch
		}
		subjects = append(subjects, s)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(subjects) != len(want) {
		return nil, nil, errSubjectsMismatch
	}
	grades := make([]Grade, len(subjects))
	for i := range subjects {
		s := &subjects[i]
		if s.SubjectID != want[i].ID || s.Position != want[i].Position {
			return nil, nil, errSubjectsMismatch
		}
		value, err := ParseNumeric(s.Grade)
		if err != nil || value.Sign() < 0 || value.Cmp(twenty) > 0 {
			return nil, nil, errSubjectsMismatch
		}
		coefficient, err := ParseNumeric(s.Coefficient)
		if err != nil || coefficient.Sign() <= 0 {
			return nil, nil, errSubjectsMismatch
		}
		if s.Method, s.Reconstructed, err = Method(s.Evidence); err != nil {
			return nil, nil, errSubjectsMismatch
		}
		grades[i] = Grade{Value: value, Coefficient: coefficient}
	}
	return subjects, grades, nil
}

func seriesRead(subjects []subject) bool {
	for _, s := range subjects {
		if s.Evidence.Series != "" {
			return true
		}
	}
	return false
}

// duplicates compares only with applications already submitted in the same campaign.
func duplicates(ctx context.Context, tx pgx.Tx, d drafts.Draft, transcriptSHA string) ([]flag, error) {
	flags := []flag{}
	for _, check := range []struct{ code, column, value string }{
		{"DUPLICATE_EMAIL", "email", d.Profile.Email},
		{"DUPLICATE_PHONE", "phone", d.Profile.Phone},
		{"DUPLICATE_TRANSCRIPT", "transcript_sha256", transcriptSHA},
	} {
		rows, err := tx.Query(ctx, `SELECT application_id::text FROM application_submissions WHERE campaign_id=$1::uuid AND `+check.column+`=$2 AND application_id<>$3::uuid ORDER BY created_at,application_id`, d.Campaign.ID, check.value, d.ID)
		if err != nil {
			return nil, err
		}
		related, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		for _, id := range related {
			flags = append(flags, flag{Code: check.code, Related: id})
		}
	}
	return flags, nil
}
