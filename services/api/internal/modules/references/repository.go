package references

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Repository interface {
	Campaigns(context.Context, string, time.Time) ([]Campaign, error)
	Program(context.Context, string) (Program, error)
	Qualification(context.Context, string) (Qualification, error)
	Programs(context.Context, string, string) ([]OfferedProgram, error)
	Qualifications(context.Context) ([]Qualification, error)
	EligibleQualifications(context.Context, string, string) ([]Qualification, error)
	Rule(context.Context, string, string, string) (Rule, error)
}
type Queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type PostgresRepository struct{ pool Queryer }

func NewRepository(pool Queryer) *PostgresRepository { return &PostgresRepository{pool} }
func (r *PostgresRepository) Campaigns(ctx context.Context, code string, now time.Time) ([]Campaign, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text,code,name,academic_year,starts_at,ends_at,status FROM admission_campaigns WHERE ($1 <> '' AND code=$1) OR ($1='' AND status='OPEN' AND starts_at <= $2 AND ends_at > $2) ORDER BY code LIMIT 2`, code, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Campaign{}
	for rows.Next() {
		var c Campaign
		if err = rows.Scan(&c.ID, &c.Code, &c.Name, &c.AcademicYear, &c.StartsAt, &c.EndsAt, &c.Status); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (r *PostgresRepository) Program(ctx context.Context, code string) (Program, error) {
	var p Program
	err := r.pool.QueryRow(ctx, `SELECT code,name FROM programs WHERE code=$1 AND enabled`, code).Scan(&p.Code, &p.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrProgramNotFound
	}
	return p, err
}
func (r *PostgresRepository) Qualification(ctx context.Context, code string) (Qualification, error) {
	var q Qualification
	err := r.pool.QueryRow(ctx, `SELECT code,label,family FROM origin_qualifications WHERE code=$1 AND enabled`, code).Scan(&q.Code, &q.Name, &q.Family)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, ErrQualificationNotFound
	}
	return q, err
}
func (r *PostgresRepository) Programs(ctx context.Context, campaign, qualification string) ([]OfferedProgram, error) {
	rows, err := r.pool.Query(ctx, `SELECT p.code,p.name,cp.available_seats FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id WHERE cp.campaign_id=$1::uuid AND cp.enabled AND p.enabled AND EXISTS (SELECT 1 FROM rule_set_versions active WHERE active.campaign_program_id=cp.id AND active.status='PUBLISHED') AND ($2='' OR EXISTS (SELECT 1 FROM rule_set_versions v JOIN program_qualification_eligibilities e ON e.rule_set_version_id=v.id JOIN origin_qualifications q ON q.id=e.origin_qualification_id WHERE v.campaign_program_id=cp.id AND v.status='PUBLISHED' AND e.eligible AND q.enabled AND q.code=$2)) ORDER BY p.code`, campaign, qualification)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []OfferedProgram{}
	for rows.Next() {
		var p OfferedProgram
		if err = rows.Scan(&p.Code, &p.Name, &p.AvailableSeats); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func (r *PostgresRepository) Qualifications(ctx context.Context) ([]Qualification, error) {
	rows, err := r.pool.Query(ctx, `SELECT code,label,family FROM origin_qualifications WHERE enabled ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanQualifications(rows)
}
func (r *PostgresRepository) EligibleQualifications(ctx context.Context, campaign, program string) ([]Qualification, error) {
	rows, err := r.pool.Query(ctx, `SELECT q.code,q.label,q.family FROM origin_qualifications q JOIN program_qualification_eligibilities e ON e.origin_qualification_id=q.id JOIN rule_set_versions v ON v.id=e.rule_set_version_id JOIN campaign_programs cp ON cp.id=v.campaign_program_id JOIN programs p ON p.id=cp.program_id WHERE cp.campaign_id=$1::uuid AND p.code=$2 AND cp.enabled AND p.enabled AND q.enabled AND e.eligible AND v.status='PUBLISHED' ORDER BY q.code`, campaign, program)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanQualifications(rows)
}
func scanQualifications(rows pgx.Rows) ([]Qualification, error) {
	result := []Qualification{}
	for rows.Next() {
		var q Qualification
		if err := rows.Scan(&q.Code, &q.Name, &q.Family); err != nil {
			return nil, err
		}
		result = append(result, q)
	}
	return result, rows.Err()
}
func (r *PostgresRepository) Rule(ctx context.Context, campaign, program, qualification string) (Rule, error) {
	// One statement gives eligibility and ordered subjects from the same PostgreSQL snapshot.
	rows, err := r.pool.Query(ctx, `SELECT e.eligible,v.version,s.code,s.name,es.position,s.enabled FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id JOIN rule_set_versions v ON v.campaign_program_id=cp.id AND v.status='PUBLISHED' JOIN program_qualification_eligibilities e ON e.rule_set_version_id=v.id JOIN origin_qualifications q ON q.id=e.origin_qualification_id LEFT JOIN expected_subject_rules es ON es.eligibility_id=e.id LEFT JOIN subjects s ON s.id=es.subject_id WHERE cp.campaign_id=$1::uuid AND p.code=$2 AND q.code=$3 AND cp.enabled AND p.enabled AND q.enabled ORDER BY es.position`, campaign, program, qualification)
	if err != nil {
		return Rule{}, err
	}
	defer rows.Close()
	result := Rule{Subjects: []ExpectedSubjectRule{}}
	for rows.Next() {
		var code, name *string
		var position *int
		var enabled *bool
		if err = rows.Scan(&result.Eligible, &result.Version, &code, &name, &position, &enabled); err != nil {
			return Rule{}, err
		}
		result.Known = true
		if code != nil {
			if enabled == nil || !*enabled {
				return Rule{}, ErrIncompleteRules
			}
			result.Subjects = append(result.Subjects, ExpectedSubjectRule{*code, *name, *position})
		}
	}
	return result, rows.Err()
}
