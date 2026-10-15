package review

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/modules/submissions"
)

// Action is one review transition requested by an administrator, at a given review version.
type Action struct {
	Version    int    `json:"version"`
	ReasonCode string `json:"reasonCode,omitempty"`
	Comment    string `json:"comment,omitempty"`
	Assignee   string `json:"assignee,omitempty"`
}

type locked struct {
	status, assignee, program string
	version                   int
	frozen                    bool
}

// lock takes the review row (always first: same order as every review write) and checks the
// optimistic version, the scope and the ranking freeze.
func (s *Service) lock(ctx context.Context, tx pgx.Tx, actor admin.Principal, id string, version int, perm admin.Permission) (locked, error) {
	var l locked
	if !validUUID(id) {
		return l, ErrNotFound
	}
	var assignee *string
	err := tx.QueryRow(ctx, `SELECT r.status,r.assignee_id::text,cp.program_id::text,r.version
	 FROM application_reviews r JOIN campaign_programs cp ON cp.id=r.campaign_program_id WHERE r.application_id=$1::uuid FOR UPDATE OF r`, id).
		Scan(&l.status, &assignee, &l.program, &l.version)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !actor.CanFor(perm, l.program)) {
		return l, ErrNotFound
	}
	if err != nil {
		return l, err
	}
	// A freeze may have committed while this transaction waited for the row: read it with a
	// fresh statement snapshot (the locking statement kept the one taken before the wait).
	if err = tx.QueryRow(ctx, `SELECT ranking_frozen(campaign_program_id) FROM application_reviews WHERE application_id=$1::uuid`, id).Scan(&l.frozen); err != nil {
		return l, err
	}
	if assignee != nil {
		l.assignee = *assignee
	}
	if l.version != version {
		return l, ErrVersion
	}
	if l.frozen {
		return l, ErrFrozen
	}
	return l, nil
}

// effective is a recomputed effective average, written with the version it belongs to.
type effective struct {
	weighted, coefficients, decimal string
	exact                           bool
}

// write moves the review to its next version (one UPDATE per version: the database refuses
// any other change) and appends the matching history event.
func write(ctx context.Context, tx pgx.Tx, id string, l locked, actor, event, status, assignee, reasonCode, comment, correction string, eff *effective) error {
	var weighted, coefficients, decimal *string
	var exact *bool
	if eff != nil {
		weighted, coefficients, decimal, exact = &eff.weighted, &eff.coefficients, &eff.decimal, &eff.exact
	}
	if _, err := tx.Exec(ctx, `UPDATE application_reviews SET status=$2,assignee_id=nullif($3,'')::uuid,version=version+1,updated_at=now(),
	 rejection_code=CASE WHEN $2='REJECTED' THEN nullif($4,'') END,rejection_comment=CASE WHEN $2='REJECTED' THEN nullif($5,'') END,
	 effective_weighted_sum=coalesce($6::numeric,effective_weighted_sum),effective_coefficient_sum=coalesce($7::numeric,effective_coefficient_sum),
	 effective_average=coalesce($8::numeric,effective_average),effective_average_is_exact=coalesce($9::boolean,effective_average_is_exact)
	 WHERE application_id=$1::uuid`, id, status, assignee, reasonCode, comment, weighted, coefficients, decimal, exact); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO application_review_events(application_id,version,event_type,actor_type,actor_id,from_status,to_status,from_assignee,to_assignee,reason_code,comment,correction_id)
	 VALUES($1::uuid,$2,$3,'ADMIN',$4::uuid,$5,$6,nullif($7,'')::uuid,nullif($8,'')::uuid,nullif($9,''),nullif($10,''),nullif($11,'')::uuid)`,
		id, l.version+1, event, actor, l.status, status, l.assignee, assignee, reasonCode, comment, correction)
	return err
}

func record(ctx context.Context, tx pgx.Tx, actor, event, id string, metadata map[string]any) error {
	return audit.Record(ctx, tx, event, audit.Actor{Type: audit.ActorAdmin, ID: actor}, "APPLICATION", id, metadata)
}

func text(value string, max int) (string, bool) {
	value = strings.TrimSpace(value)
	return value, value != "" && utf8.RuneCountInString(value) <= max && utf8.ValidString(value)
}

// Transition runs claim, validate, reject, reassign or revert in one transaction.
func (s *Service) Transition(ctx context.Context, actor admin.Principal, id, action string, input Action) (Detail, error) {
	perms := map[string]admin.Permission{"claim": admin.PermReviewClaim, "validate": admin.PermReviewDecide, "reject": admin.PermReviewDecide,
		"reassign": admin.PermReviewReassign, "revert": admin.PermReviewRevert}
	perm, known := perms[action]
	if !known || input.Version < 1 {
		return Detail{}, admin.NewProblem(400, "INVALID_PARAMETERS", "Action de revue invalide.")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	l, err := s.lock(ctx, tx, actor, id, input.Version, perm)
	if err != nil {
		return Detail{}, err
	}
	me := actor.UserID
	comment, hasComment := text(input.Comment, 2000)
	metadata := map[string]any{"version": l.version + 1}
	switch action {
	case "claim":
		if l.status != StatusToReview {
			return Detail{}, ErrTransition
		}
		err = write(ctx, tx, id, l, me, "CLAIMED", StatusInReview, me, "", "", "", nil)
	case "validate", "reject":
		if l.status != StatusInReview {
			return Detail{}, ErrTransition
		}
		if l.assignee != me {
			return Detail{}, ErrNotAssignee
		}
		if action == "validate" {
			var pending bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM application_corrections WHERE application_id=$1::uuid AND status='PENDING')`, id).Scan(&pending); err != nil {
				return Detail{}, err
			}
			if pending {
				return Detail{}, ErrPendingCorrection
			}
			err = write(ctx, tx, id, l, me, "VALIDATED", StatusValidated, me, "", "", "", nil)
			break
		}
		var known bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_rejection_reasons WHERE code=$1 AND enabled)`, input.ReasonCode).Scan(&known); err != nil {
			return Detail{}, err
		}
		if !known || !hasComment {
			fields := map[string]string{}
			if !known {
				fields["reasonCode"] = "Choisissez un motif de rejet."
			}
			if !hasComment {
				fields["comment"] = "Expliquez le rejet (2 000 caractères au plus)."
			}
			return Detail{}, invalid(fields)
		}
		metadata["reasonCode"] = input.ReasonCode
		err = write(ctx, tx, id, l, me, "REJECTED", StatusRejected, me, input.ReasonCode, comment, "", nil)
	case "reassign":
		if l.status != StatusInReview {
			return Detail{}, ErrTransition
		}
		if !hasComment {
			return Detail{}, invalid(map[string]string{"comment": "Indiquez le motif de la réattribution."})
		}
		if input.Assignee == l.assignee || !validUUID(input.Assignee) {
			return Detail{}, invalid(map[string]string{"assignee": "Choisissez un autre agent."})
		}
		candidates, agentsErr := agents(ctx, tx)
		if agentsErr != nil {
			return Detail{}, agentsErr
		}
		found := false
		for _, a := range candidates {
			found = found || a.ID == input.Assignee
		}
		if !found {
			return Detail{}, invalid(map[string]string{"assignee": "Cet agent ne peut pas prendre de dossier en charge."})
		}
		metadata["fromAssignee"], metadata["toAssignee"] = l.assignee, input.Assignee
		err = write(ctx, tx, id, l, me, "REASSIGNED", StatusInReview, input.Assignee, "", comment, "", nil)
	case "revert":
		if l.status != StatusValidated && l.status != StatusRejected {
			return Detail{}, ErrTransition
		}
		if !hasComment {
			return Detail{}, invalid(map[string]string{"comment": "Indiquez le motif du retour en vérification."})
		}
		metadata["fromStatus"] = l.status
		err = write(ctx, tx, id, l, me, "REVERTED", StatusInReview, l.assignee, "", comment, "", nil)
	}
	if err != nil {
		return Detail{}, err
	}
	if err = record(ctx, tx, me, map[string]string{"claim": "REVIEW_CLAIMED", "validate": "REVIEW_VALIDATED", "reject": "REVIEW_REJECTED",
		"reassign": "REVIEW_REASSIGNED", "revert": "REVIEW_REVERTED"}[action], id, metadata); err != nil {
		return Detail{}, err
	}
	d, err := s.detail(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	return d, tx.Commit(ctx)
}

// Proposal is a correction of one grade or coefficient by the agent holding the dossier.
type Proposal struct {
	Version   int    `json:"version"`
	SubjectID string `json:"subjectId"`
	Field     string `json:"field"`
	Value     string `json:"value"`
	Reason    string `json:"reason"`
}

var (
	gradePattern       = regexp.MustCompile(`^[0-9]{1,2}(\.[0-9]{1,3})?$`)
	coefficientPattern = regexp.MustCompile(`^[0-9]{1,2}(\.[0-9]{1,2})?$`)
	twenty             = big.NewRat(20, 1)
)

func (s *Service) Propose(ctx context.Context, actor admin.Principal, id string, input Proposal) (Detail, error) {
	value := strings.ReplaceAll(strings.TrimSpace(input.Value), ",", ".")
	reason, hasReason := text(input.Reason, 2000)
	fields := map[string]string{}
	pattern := map[string]*regexp.Regexp{"GRADE": gradePattern, "COEFFICIENT": coefficientPattern}[input.Field]
	if pattern == nil || !validUUID(input.SubjectID) {
		return Detail{}, admin.NewProblem(400, "INVALID_PARAMETERS", "Correction invalide.")
	}
	proposed, err := submissions.ParseNumeric(value)
	if !pattern.MatchString(value) || err != nil || (input.Field == "GRADE" && proposed.Cmp(twenty) > 0) || (input.Field == "COEFFICIENT" && proposed.Sign() <= 0) {
		fields["value"] = map[string]string{"GRADE": "Saisissez une note entre 0 et 20 (3 décimales au plus).", "COEFFICIENT": "Saisissez un coefficient positif (2 décimales au plus)."}[input.Field]
	}
	if !hasReason {
		fields["reason"] = "Expliquez la correction (2 000 caractères au plus)."
	}
	if len(fields) > 0 {
		return Detail{}, invalid(fields)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	l, err := s.lock(ctx, tx, actor, id, input.Version, admin.PermCorrectionsPropose)
	if err != nil {
		return Detail{}, err
	}
	if l.status != StatusInReview {
		return Detail{}, ErrTransition
	}
	if l.assignee != actor.UserID {
		return Detail{}, ErrNotAssignee
	}
	current, err := subjects(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	var target *SubjectView
	for i := range current {
		if current[i].SubjectID == input.SubjectID {
			target = &current[i]
		}
	}
	if target == nil {
		return Detail{}, admin.NewProblem(404, "SUBJECT_NOT_FOUND", "Cette matière n’appartient pas au dossier.")
	}
	original, effective := target.Grade, target.EffectiveGrade
	if input.Field == "COEFFICIENT" {
		original, effective = target.Coefficient, target.EffectiveCoefficient
	}
	if now, _ := submissions.ParseNumeric(effective); now.Cmp(proposed) == 0 {
		return Detail{}, invalid(map[string]string{"value": "La valeur proposée est déjà la valeur retenue."})
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM application_corrections WHERE application_id=$1::uuid AND subject_id=$2::uuid AND field=$3 AND status='PENDING')`, id, input.SubjectID, input.Field).Scan(&pending); err != nil {
		return Detail{}, err
	}
	if pending {
		return Detail{}, admin.NewProblem(409, "CORRECTION_ALREADY_PENDING", "Une correction de cette valeur attend déjà une décision.")
	}
	var correction string
	if err = tx.QueryRow(ctx, `INSERT INTO application_corrections(application_id,submission_id,subject_id,field,original_value,current_value,proposed_value,reason,proposed_by)
	 SELECT $1::uuid,s.id,$2::uuid,$3,$4::numeric,$5::numeric,$6::numeric,$7,$8::uuid FROM application_submissions s WHERE s.application_id=$1::uuid RETURNING id::text`,
		id, input.SubjectID, input.Field, original, effective, value, reason, actor.UserID).Scan(&correction); err != nil {
		return Detail{}, err
	}
	if err = write(ctx, tx, id, l, actor.UserID, "CORRECTION_PROPOSED", l.status, l.assignee, "", reason, correction, nil); err != nil {
		return Detail{}, err
	}
	if err = record(ctx, tx, actor.UserID, "CORRECTION_PROPOSED", id, map[string]any{"correctionId": correction, "field": input.Field, "subjectId": input.SubjectID}); err != nil {
		return Detail{}, err
	}
	d, err := s.detail(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	return d, tx.Commit(ctx)
}

// Decision approves or refuses a pending correction (deputy director, never its author).
type Decision struct {
	Version int    `json:"version"`
	Reason  string `json:"reason,omitempty"`
}

func (s *Service) Decide(ctx context.Context, actor admin.Principal, correction string, approve bool, input Decision) (Detail, error) {
	if !validUUID(correction) {
		return Detail{}, ErrCorrectionMissing
	}
	reason, hasReason := text(input.Reason, 2000)
	if !approve && !hasReason {
		return Detail{}, invalid(map[string]string{"reason": "Indiquez le motif du refus."})
	}
	var id string
	if err := s.Pool.QueryRow(ctx, `SELECT application_id::text FROM application_corrections WHERE id=$1::uuid`, correction).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrCorrectionMissing
	} else if err != nil {
		return Detail{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	l, err := s.lock(ctx, tx, actor, id, input.Version, admin.PermCorrectionsDecide)
	if err != nil {
		return Detail{}, err
	}
	var status, author, field, subject string
	if err = tx.QueryRow(ctx, `SELECT status,proposed_by::text,field,subject_id::text FROM application_corrections WHERE id=$1::uuid FOR UPDATE`, correction).Scan(&status, &author, &field, &subject); err != nil {
		return Detail{}, err
	}
	if status != "PENDING" {
		return Detail{}, ErrCorrectionState
	}
	if author == actor.UserID {
		return Detail{}, ErrFourEyes
	}
	decision, event := "REFUSED", "CORRECTION_REFUSED"
	if approve {
		decision, event = "APPROVED", "CORRECTION_APPROVED"
	}
	if _, err = tx.Exec(ctx, `UPDATE application_corrections SET status=$2,decided_by=$3::uuid,decided_at=now(),decision_reason=nullif($4,'') WHERE id=$1::uuid`, correction, decision, actor.UserID, reason); err != nil {
		return Detail{}, err
	}
	metadata := map[string]any{"correctionId": correction, "field": field, "subjectId": subject}
	var eff *effective
	if approve {
		if eff, err = recompute(ctx, tx, id); err != nil {
			return Detail{}, err
		}
		metadata["effectiveAverageExact"] = eff.exact
	}
	if err = write(ctx, tx, id, l, actor.UserID, event, l.status, l.assignee, "", reason, correction, eff); err != nil {
		return Detail{}, err
	}
	if err = record(ctx, tx, actor.UserID, event, id, metadata); err != nil {
		return Detail{}, err
	}
	d, err := s.detail(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	return d, tx.Commit(ctx)
}

// recompute derives the effective average exactly (phase 6 method) from the snapshot values
// with the latest approved corrections applied; the database re-checks the sums at commit.
func recompute(ctx context.Context, tx pgx.Tx, id string) (*effective, error) {
	list, err := subjects(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	grades := make([]submissions.Grade, 0, len(list))
	for _, v := range list {
		g, err1 := submissions.ParseNumeric(v.EffectiveGrade)
		c, err2 := submissions.ParseNumeric(v.EffectiveCoefficient)
		if err1 != nil || err2 != nil {
			return nil, errors.New("effective value unreadable")
		}
		grades = append(grades, submissions.Grade{Value: g, Coefficient: c})
	}
	avg, err := submissions.ComputeAverage(grades)
	if err != nil {
		return nil, err
	}
	eff := &effective{decimal: avg.Decimal, exact: avg.Exact}
	if eff.weighted, err = submissions.ExactDecimal(avg.WeightedSum); err != nil {
		return nil, err
	}
	if eff.coefficients, err = submissions.ExactDecimal(avg.CoefficientSum); err != nil {
		return nil, err
	}
	return eff, nil
}
