package review

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/modules/analysis"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/modules/submissions"
	"ifri-admissions/services/api/internal/platform/storage"
)

type Service struct {
	Pool    *pgxpool.Pool
	Objects storage.TypedObjects
	Cache   *redis.Client
	Logger  *slog.Logger
	// Stream overrides ProofStream (tests use their own stream).
	Stream string
}

func (s *Service) stream() string {
	if s.Stream != "" {
		return s.Stream
	}
	return ProofStream
}

func validUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// average renders exact sums; nil when the snapshot has no average (future manual path).
func average(weighted, coefficients *string) *Average {
	if weighted == nil || coefficients == nil {
		return nil
	}
	w, err1 := submissions.ParseNumeric(*weighted)
	c, err2 := submissions.ParseNumeric(*coefficients)
	if err1 != nil || err2 != nil || c.Sign() <= 0 {
		return nil
	}
	fraction := new(big.Rat).Quo(w, c)
	display, _ := submissions.Truncate(fraction, 2)
	decimal, exact := submissions.Truncate(fraction, submissions.AveragePlaces)
	return &Average{Display: display, Decimal: decimal, Exact: exact, WeightedSum: *weighted, CoefficientSum: *coefficients}
}

// scope returns the programs the reader may see (nil = all, the IFRI organisation).
func scope(p admin.Principal) []string {
	all, programs := p.Scope(admin.PermApplicationsRead)
	if all {
		return nil
	}
	if programs == nil {
		return []string{}
	}
	return programs
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

var sorts = map[string]string{
	"average":     "r.effective_average",
	"submittedAt": "a.submitted_at",
	"name":        "s.name_key",
}

// List pages, sorts and filters on the server; contact details are never part of the list.
func (s *Service) List(ctx context.Context, reader admin.Principal, f Filter) (ListPage, error) {
	column, ok := sorts[f.Sort]
	if f.Sort == "" {
		column, ok = sorts["submittedAt"], true
	}
	direction := "DESC"
	if f.Direction == "asc" {
		direction = "ASC"
	} else if f.Direction != "" && f.Direction != "desc" {
		ok = false
	}
	statuses := map[string]bool{"": true, StatusToReview: true, StatusInReview: true, StatusValidated: true, StatusRejected: true}
	flags := map[string]bool{"": true, "ANY": true, "DUPLICATE_EMAIL": true, "DUPLICATE_PHONE": true, "DUPLICATE_TRANSCRIPT": true, "SERIES_UNREADABLE": true}
	if !ok || !statuses[f.Status] || !flags[f.Flag] || len(f.Query) > 120 || (f.Assignee != "" && f.Assignee != "none" && f.Assignee != "me" && !validUUID(f.Assignee)) {
		return ListPage{}, admin.NewProblem(400, "INVALID_PARAMETERS", "Les filtres de la liste sont invalides.")
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 25
	}
	if f.Page < 1 {
		f.Page = 1
	}
	assignee := f.Assignee
	if assignee == "me" {
		assignee = reader.UserID
	}
	query := strings.TrimSpace(f.Query)
	var number, email string
	words := []string{}
	switch {
	case strings.Contains(query, "@"):
		email = strings.ToLower(query)
	case strings.HasPrefix(strings.ToUpper(query), "IFRI-"):
		number = escapeLike(strings.ToUpper(query)) + "%"
	case query != "":
		for _, word := range strings.Fields(query) {
			words = append(words, escapeLike(word))
		}
	}
	rows, err := s.Pool.Query(ctx, `SELECT a.id::text,a.application_number,a.submitted_at,s.last_name,s.first_names,q.code,q.label,p.code,p.name,r.status,
	 coalesce(r.assignee_id::text,''),coalesce(u.display_name,''),r.effective_weighted_sum::text,r.effective_coefficient_sum::text,
	 coalesce((SELECT array_agg(DISTINCT f.code ORDER BY f.code) FROM application_submission_flags f WHERE f.application_id=a.id),'{}'),
	 (SELECT count(*) FROM application_corrections k WHERE k.application_id=a.id AND k.status='PENDING'),
	 count(*) OVER()
	 FROM application_reviews r
	 JOIN applications a ON a.id=r.application_id
	 JOIN application_submissions s ON s.application_id=r.application_id
	 JOIN programs p ON p.id=s.program_id
	 JOIN origin_qualifications q ON q.id=s.origin_qualification_id
	 LEFT JOIN admin_users u ON u.id=r.assignee_id
	 WHERE ($1::uuid[] IS NULL OR s.program_id=ANY($1::uuid[]))
	 AND ($2='' OR p.code=$2) AND ($3='' OR q.code=$3) AND ($4='' OR r.status=$4)
	 AND ($5='' OR ($5='none' AND r.assignee_id IS NULL) OR ($5<>'none' AND r.assignee_id=nullif(nullif($5,'none'),'')::uuid))
	 AND ($6='' OR ($6='ANY' AND EXISTS (SELECT 1 FROM application_submission_flags f WHERE f.application_id=a.id))
	  OR EXISTS (SELECT 1 FROM application_submission_flags f WHERE f.application_id=a.id AND f.code=$6))
	 AND (NOT $7 OR EXISTS (SELECT 1 FROM application_corrections k WHERE k.application_id=a.id AND k.status='PENDING'))
	 AND ($8='' OR s.email=$8) AND ($9='' OR a.application_number LIKE $9)
	 AND (cardinality($10::text[])=0 OR s.name_key LIKE ALL (SELECT '%'||ifri_fold(w)||'%' FROM unnest($10::text[]) w))
	 ORDER BY `+column+` `+direction+` NULLS LAST,a.application_number `+direction+`
	 LIMIT $11 OFFSET $12`,
		scope(reader), f.Program, f.Qualification, f.Status, assignee, f.Flag, f.PendingCorrection, email, number, words, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return ListPage{}, err
	}
	page := ListPage{Items: []ListItem{}, Page: f.Page, PageSize: f.PageSize}
	for rows.Next() {
		var item ListItem
		var submitted time.Time
		var assigneeID, assigneeName string
		var weighted, coefficients *string
		if err = rows.Scan(&item.ID, &item.Number, &submitted, &item.LastName, &item.FirstNames, &item.Qualification, &item.QualificationLabel, &item.Program, &item.ProgramName,
			&item.Status, &assigneeID, &assigneeName, &weighted, &coefficients, &item.Flags, &item.PendingCorrections, &page.Total); err != nil {
			rows.Close()
			return ListPage{}, err
		}
		item.SubmittedAt = submitted.UTC().Format(time.RFC3339)
		item.Average = average(weighted, coefficients)
		if assigneeID != "" {
			item.Assignee = &Person{ID: assigneeID, Name: assigneeName}
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func person(id, name *string) *Person {
	if id == nil || *id == "" {
		return nil
	}
	p := &Person{ID: *id}
	if name != nil {
		p.Name = *name
	}
	return p
}

// visible checks that the dossier exists and lies in the reader's scope.
func (s *Service) visible(ctx context.Context, q pgx.Tx, reader admin.Principal, id string) error {
	if !validUUID(id) {
		return ErrNotFound
	}
	var program string
	err := q.QueryRow(ctx, `SELECT s.program_id::text FROM application_submissions s WHERE s.application_id=$1::uuid`, id).Scan(&program)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !reader.CanFor(admin.PermApplicationsRead, program)) {
		return ErrNotFound
	}
	return err
}

// Detail opens a dossier and traces the consultation in the same transaction.
func (s *Service) Detail(ctx context.Context, reader admin.Principal, id string) (Detail, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = s.visible(ctx, tx, reader, id); err != nil {
		return Detail{}, err
	}
	d, err := s.detail(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	if err = audit.RecordAccess(ctx, tx, reader.UserID, "APPLICATION", id, "REVIEW_DETAIL"); err != nil {
		return Detail{}, err
	}
	return d, tx.Commit(ctx)
}

func (s *Service) detail(ctx context.Context, tx pgx.Tx, id string) (Detail, error) {
	d := Detail{ID: id}
	var assigneeID, assigneeName, rejectionCode, rejectionLabel, rejectionComment *string
	var snapW, snapC, effW, effC *string
	var analysisID string
	err := tx.QueryRow(ctx, `SELECT a.application_number,a.submitted_at,k.name,s.last_name,s.first_names,s.email,s.phone,q.code,q.label,s.graduation_year,p.code,p.name,
	 s.weighted_sum::text,s.coefficient_sum::text,r.effective_weighted_sum::text,r.effective_coefficient_sum::text,r.status,r.assignee_id::text,u.display_name,r.version,
	 r.rejection_code,rr.label,r.rejection_comment,ranking_frozen(r.campaign_program_id),coalesce(s.ocr_analysis_id::text,'')
	 FROM application_submissions s JOIN applications a ON a.id=s.application_id JOIN admission_campaigns k ON k.id=s.campaign_id
	 JOIN origin_qualifications q ON q.id=s.origin_qualification_id JOIN programs p ON p.id=s.program_id
	 JOIN application_reviews r ON r.application_id=s.application_id LEFT JOIN admin_users u ON u.id=r.assignee_id
	 LEFT JOIN review_rejection_reasons rr ON rr.code=r.rejection_code WHERE s.application_id=$1::uuid`, id).Scan(
		&d.Number, &d.SubmittedAt, &d.Campaign, &d.LastName, &d.FirstNames, &d.Email, &d.Phone, &d.Qualification, &d.QualificationLabel, &d.GraduationYear, &d.Program, &d.ProgramName,
		&snapW, &snapC, &effW, &effC, &d.Status, &assigneeID, &assigneeName, &d.Version, &rejectionCode, &rejectionLabel, &rejectionComment, &d.Frozen, &analysisID)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.SnapshotAverage, d.EffectiveAverage = average(snapW, snapC), average(effW, effC)
	d.Assignee = person(assigneeID, assigneeName)
	for target, value := range map[*string]*string{&d.RejectionCode: rejectionCode, &d.RejectionLabel: rejectionLabel, &d.RejectionComment: rejectionComment} {
		if value != nil {
			*target = *value
		}
	}
	d.DeclaredSeries = analysis.DeclaredSeries(d.Qualification)
	if analysisID != "" {
		if err = tx.QueryRow(ctx, `SELECT coalesce(max(evidence->>'series'),'') FROM ocr_extracted_subjects WHERE ocr_analysis_id=$1::uuid AND coalesce(evidence->>'series','')<>''`, analysisID).Scan(&d.ReadSeries); err != nil {
			return d, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT sd.document_type,v.original_filename,v.size_bytes,v.version_number FROM application_submission_documents sd
	 JOIN application_submissions s ON s.id=sd.submission_id JOIN document_versions v ON v.id=sd.document_version_id
	 WHERE s.application_id=$1::uuid ORDER BY array_position(ARRAY['REQUEST_LETTER','TRANSCRIPT','IDENTITY_DOCUMENT','PARENTAL_COMMITMENT'],sd.document_type)`, id)
	if err != nil {
		return d, err
	}
	if d.Documents, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DocumentView, error) {
		var v DocumentView
		return v, row.Scan(&v.Type, &v.Filename, &v.SizeBytes, &v.Version)
	}); err != nil {
		return d, err
	}
	if d.Subjects, err = subjects(ctx, tx, id); err != nil {
		return d, err
	}
	if d.Corrections, err = corrections(ctx, tx, `k.application_id=$1::uuid`, id); err != nil {
		return d, err
	}
	rows, err = tx.Query(ctx, `SELECT e.version,e.event_type,e.actor_id::text,ua.display_name,coalesce(e.from_status,''),e.to_status,e.from_assignee::text,uf.display_name,
	 e.to_assignee::text,ut.display_name,coalesce(e.reason_code,''),coalesce(e.comment,''),coalesce(e.correction_id::text,''),e.occurred_at
	 FROM application_review_events e LEFT JOIN admin_users ua ON ua.id=e.actor_id LEFT JOIN admin_users uf ON uf.id=e.from_assignee
	 LEFT JOIN admin_users ut ON ut.id=e.to_assignee WHERE e.application_id=$1::uuid ORDER BY e.version`, id)
	if err != nil {
		return d, err
	}
	if d.History, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (EventView, error) {
		var e EventView
		var actorID, actorName, fromID, fromName, toID, toName *string
		err := row.Scan(&e.Version, &e.Type, &actorID, &actorName, &e.FromStatus, &e.ToStatus, &fromID, &fromName, &toID, &toName, &e.ReasonCode, &e.Comment, &e.CorrectionID, &e.OccurredAt)
		e.Actor, e.FromAssignee, e.ToAssignee = person(actorID, actorName), person(fromID, fromName), person(toID, toName)
		return e, err
	}); err != nil {
		return d, err
	}
	// Duplicates in both directions: the first dossier of a pair carries no flag itself.
	rows, err = tx.Query(ctx, `SELECT f.code,'OUTGOING',coalesce(f.related_application_id::text,''),coalesce(ra.application_number,'')
	 FROM application_submission_flags f LEFT JOIN applications ra ON ra.id=f.related_application_id WHERE f.application_id=$1::uuid
	 UNION ALL SELECT f.code,'INCOMING',f.application_id::text,fa.application_number
	 FROM application_submission_flags f JOIN applications fa ON fa.id=f.application_id WHERE f.related_application_id=$1::uuid
	 ORDER BY 2 DESC,1,4`, id)
	if err != nil {
		return d, err
	}
	d.Flags, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (FlagView, error) {
		var f FlagView
		return f, row.Scan(&f.Code, &f.Direction, &f.ApplicationID, &f.Number)
	})
	return d, err
}

// effectiveSQL applies the latest approved correction of each field to the snapshot values
// (same rule as the database check of migration 00008).
const effectiveSQL = `SELECT x.subject_id::text,sub.code,x.subject_name,x.position,x.grade_raw,x.grade_value::text,x.coefficient_raw,x.coefficient_value::text,
 coalesce((SELECT k.proposed_value FROM application_corrections k WHERE k.application_id=s.application_id AND k.subject_id=x.subject_id AND k.field='GRADE' AND k.status='APPROVED' ORDER BY k.decided_at DESC,k.id DESC LIMIT 1),x.grade_value)::text,
 coalesce((SELECT k.proposed_value FROM application_corrections k WHERE k.application_id=s.application_id AND k.subject_id=x.subject_id AND k.field='COEFFICIENT' AND k.status='APPROVED' ORDER BY k.decided_at DESC,k.id DESC LIMIT 1),x.coefficient_value)::text,
 x.validation_method,x.reconstructed_cells,coalesce(pi.status,'')
 FROM application_submission_subjects x JOIN application_submissions s ON s.id=x.submission_id JOIN subjects sub ON sub.id=x.subject_id
 LEFT JOIN ocr_proof_images pi ON pi.ocr_extracted_subject_id=x.ocr_extracted_subject_id
 WHERE s.application_id=$1::uuid ORDER BY x.position`

func subjects(ctx context.Context, tx pgx.Tx, id string) ([]SubjectView, error) {
	rows, err := tx.Query(ctx, effectiveSQL, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SubjectView, error) {
		var v SubjectView
		return v, row.Scan(&v.SubjectID, &v.Code, &v.Name, &v.Position, &v.GradeRaw, &v.Grade, &v.CoefficientRaw, &v.Coefficient,
			&v.EffectiveGrade, &v.EffectiveCoefficient, &v.Method, &v.Reconstructed, &v.ProofStatus)
	})
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func corrections(ctx context.Context, q queryer, where string, args ...any) ([]CorrectionView, error) {
	rows, err := q.Query(ctx, `SELECT k.id::text,k.application_id::text,a.application_number,s.last_name||' '||s.first_names,k.subject_id::text,x.subject_name,k.field,
	 k.original_value::text,k.current_value::text,k.proposed_value::text,k.reason,k.proposed_by::text,up.display_name,k.proposed_at,k.status,
	 k.decided_by::text,ud.display_name,k.decided_at,coalesce(k.decision_reason,''),r.version
	 FROM application_corrections k JOIN applications a ON a.id=k.application_id JOIN application_submissions s ON s.application_id=k.application_id
	 JOIN application_submission_subjects x ON x.submission_id=k.submission_id AND x.subject_id=k.subject_id
	 JOIN application_reviews r ON r.application_id=k.application_id
	 JOIN admin_users up ON up.id=k.proposed_by LEFT JOIN admin_users ud ON ud.id=k.decided_by
	 WHERE `+where+` ORDER BY k.proposed_at,k.id`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CorrectionView, error) {
		var c CorrectionView
		var deciderID, deciderName *string
		err := row.Scan(&c.ID, &c.ApplicationID, &c.Number, &c.Candidate, &c.SubjectID, &c.SubjectName, &c.Field, &c.OriginalValue, &c.CurrentValue, &c.ProposedValue,
			&c.Reason, &c.ProposedBy.ID, &c.ProposedBy.Name, &c.ProposedAt, &c.Status, &deciderID, &deciderName, &c.DecidedAt, &c.DecisionReason, &c.ReviewVersion)
		c.DecidedBy = person(deciderID, deciderName)
		return c, err
	})
}

// PendingCorrections is the deputy director's queue, oldest first.
func (s *Service) PendingCorrections(ctx context.Context, reader admin.Principal) ([]CorrectionView, error) {
	list, err := corrections(ctx, s.Pool, `k.status='PENDING' AND ($1::uuid[] IS NULL OR s.program_id=ANY($1::uuid[]))`, scope(reader))
	if list == nil {
		list = []CorrectionView{}
	}
	return list, err
}

func (s *Service) Dashboard(ctx context.Context, reader admin.Principal, accounts *admin.Service) (Dashboard, error) {
	var d Dashboard
	if reader.Can(admin.PermApplicationsRead) {
		rows, err := s.Pool.Query(ctx, `SELECT p.code,p.name,r.status,count(*)::int FROM application_reviews r JOIN campaign_programs cp ON cp.id=r.campaign_program_id
		 JOIN programs p ON p.id=cp.program_id WHERE ($1::uuid[] IS NULL OR p.id=ANY($1::uuid[])) GROUP BY p.code,p.name,r.status ORDER BY p.code,r.status`, scope(reader))
		if err != nil {
			return d, err
		}
		if d.Counters, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Counter, error) {
			var c Counter
			return c, row.Scan(&c.Program, &c.ProgramName, &c.Status, &c.Count)
		}); err != nil {
			return d, err
		}
		var pending, unassigned, mine int
		if err = s.Pool.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM application_corrections k JOIN application_submissions s ON s.application_id=k.application_id WHERE k.status='PENDING' AND ($1::uuid[] IS NULL OR s.program_id=ANY($1::uuid[]))),
		 (SELECT count(*) FROM application_reviews r JOIN campaign_programs cp ON cp.id=r.campaign_program_id WHERE r.status='TO_REVIEW' AND ($1::uuid[] IS NULL OR cp.program_id=ANY($1::uuid[]))),
		 (SELECT count(*) FROM application_reviews WHERE status='IN_REVIEW' AND assignee_id=$2::uuid)`, scope(reader), reader.UserID).Scan(&pending, &unassigned, &mine); err != nil {
			return d, err
		}
		d.PendingCorrections, d.Unassigned = &pending, &unassigned
		if reader.Can(admin.PermReviewClaim) {
			d.Mine = &mine
		}
	}
	if reader.Can(admin.PermAccountsRead) {
		coverage, err := accounts.Coverage(ctx)
		if err != nil {
			return d, err
		}
		d.Coverage = &coverage
	}
	return d, nil
}

func (s *Service) Reference(ctx context.Context, reader admin.Principal) (Reference, error) {
	ref := Reference{RejectionReasons: []Reason{}, Agents: []Person{}}
	rows, err := s.Pool.Query(ctx, `SELECT code,label FROM review_rejection_reasons WHERE enabled ORDER BY position`)
	if err != nil {
		return ref, err
	}
	if ref.RejectionReasons, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reason, error) {
		var r Reason
		return r, row.Scan(&r.Code, &r.Label)
	}); err != nil {
		return ref, err
	}
	// Agents' names serve the "agent en charge" filter and the reassignment list.
	if ref.Agents, err = agents(ctx, s.Pool); err != nil {
		return ref, err
	}
	for target, query := range map[*[]Reason]string{
		&ref.Programs:       `SELECT code,name FROM programs ORDER BY code`,
		&ref.Qualifications: `SELECT code,label FROM origin_qualifications ORDER BY code`,
	} {
		rows, err := s.Pool.Query(ctx, query)
		if err != nil {
			return ref, err
		}
		if *target, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reason, error) {
			var r Reason
			return r, row.Scan(&r.Code, &r.Label)
		}); err != nil {
			return ref, err
		}
	}
	return ref, nil
}

// agents can hold dossiers: active accounts whose role grants review.claim.
func agents(ctx context.Context, q queryer) ([]Person, error) {
	roles := []string{}
	for role, perms := range admin.RolePermissions {
		for _, p := range perms {
			if p == admin.PermReviewClaim {
				roles = append(roles, string(role))
			}
		}
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT u.id::text,u.display_name FROM admin_users u JOIN admin_role_assignments r ON r.admin_user_id=u.id AND r.revoked_at IS NULL
	 WHERE u.status='ACTIVE' AND r.role=ANY($1) ORDER BY 2,1`, roles)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Person, error) {
		var p Person
		return p, row.Scan(&p.ID, &p.Name)
	})
}

// Document returns the exact submitted version of one document, tracing the consultation.
func (s *Service) Document(ctx context.Context, reader admin.Principal, id, documentType string) (DocumentView, storage.Object, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return DocumentView{}, storage.Object{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = s.visible(ctx, tx, reader, id); err != nil {
		return DocumentView{}, storage.Object{}, err
	}
	var v DocumentView
	var key string
	err = tx.QueryRow(ctx, `SELECT sd.document_type,v.original_filename,v.size_bytes,v.version_number,v.storage_key FROM application_submission_documents sd
	 JOIN application_submissions s ON s.id=sd.submission_id JOIN document_versions v ON v.id=sd.document_version_id
	 WHERE s.application_id=$1::uuid AND sd.document_type=$2`, id, documentType).Scan(&v.Type, &v.Filename, &v.SizeBytes, &v.Version, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, storage.Object{}, ErrDocument
	}
	if err != nil {
		return v, storage.Object{}, err
	}
	if err = audit.RecordAccess(ctx, tx, reader.UserID, "APPLICATION", id, "DOCUMENT:"+documentType); err != nil {
		return v, storage.Object{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return v, storage.Object{}, err
	}
	obj, err := s.Objects.Get(ctx, key)
	if err != nil {
		return v, storage.Object{}, ErrStorage
	}
	if obj.Size != v.SizeBytes || obj.ContentType != "application/pdf" {
		obj.Body.Close()
		return v, storage.Object{}, ErrStorage
	}
	return v, obj, nil
}
