package drafts

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ifri-admissions/services/api/internal/modules/references"
)

type Repository interface {
	Create(context.Context, string, []byte, time.Duration) (Draft, error)
	Current(context.Context, []byte) (Draft, error)
	Update(context.Context, []byte, string, Change) (Draft, error)
}
type PostgresRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PostgresRepository { return &PostgresRepository{pool} }

const readDraft = `SELECT a.id::text,c.last_name,c.first_names,c.email,c.phone,
 campaign.id::text,campaign.code,campaign.name,campaign.academic_year,campaign.starts_at,campaign.ends_at,campaign.status,
 q.code,q.label,q.family,a.graduation_year,p.code,p.name,a.state,a.version,a.updated_at,s.expires_at,s.revoked_at
 FROM application_draft_sessions s JOIN applications a ON a.id=s.application_id
 JOIN candidates c ON c.id=a.candidate_id JOIN admission_campaigns campaign ON campaign.id=a.campaign_id
 LEFT JOIN origin_qualifications q ON q.id=a.origin_qualification_id
 LEFT JOIN campaign_programs cp ON cp.id=a.campaign_program_id LEFT JOIN programs p ON p.id=cp.program_id
 WHERE s.secret_hash=$1`

func read(ctx context.Context, db references.Queryer, hash []byte, lock bool) (Draft, error) {
	query := readDraft
	if lock {
		query += " FOR UPDATE OF a,s"
	}
	var d Draft
	var qcode, qname, qfamily, pcode, pname *string
	var revoked *time.Time
	err := db.QueryRow(ctx, query, hash).Scan(&d.ID, &d.Profile.LastName, &d.Profile.FirstNames, &d.Profile.Email, &d.Profile.Phone, &d.Campaign.ID, &d.Campaign.Code, &d.Campaign.Name, &d.Campaign.AcademicYear, &d.Campaign.StartsAt, &d.Campaign.EndsAt, &d.Campaign.Status, &qcode, &qname, &qfamily, &d.GraduationYear, &pcode, &pname, &d.State, &d.Version, &d.UpdatedAt, &d.SessionExpiresAt, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrSession
	}
	if err != nil {
		return d, err
	}
	if revoked != nil {
		return Draft{}, ErrRevoked
	}
	if !time.Now().Before(d.SessionExpiresAt) {
		return Draft{}, ErrExpired
	}
	if qcode != nil {
		d.Qualification = &references.Qualification{Code: *qcode, Name: *qname, Family: *qfamily}
	}
	if pcode != nil {
		d.Program = &references.Program{Code: *pcode, Name: *pname}
	}
	return d, nil
}
func (r *PostgresRepository) Current(ctx context.Context, hash []byte) (Draft, error) {
	return read(ctx, r.pool, hash, false)
}
func (r *PostgresRepository) Create(ctx context.Context, campaign string, hash []byte, ttl time.Duration) (Draft, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Draft{}, err
	}
	defer tx.Rollback(ctx)
	c, err := references.NewService(references.NewRepository(tx)).Campaign(ctx, campaign)
	if err != nil {
		return Draft{}, err
	}
	// Coordinate creation with future campaign configuration writes.
	if _, err = tx.Exec(ctx, `SELECT id FROM admission_campaigns WHERE id=$1::uuid FOR SHARE`, c.ID); err != nil {
		return Draft{}, err
	}
	c, err = references.NewService(references.NewRepository(tx)).Campaign(ctx, c.Code)
	if err != nil {
		return Draft{}, err
	}
	var candidate, application string
	if err = tx.QueryRow(ctx, `INSERT INTO candidates DEFAULT VALUES RETURNING id::text`).Scan(&candidate); err != nil {
		return Draft{}, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO applications(candidate_id,campaign_id) VALUES($1::uuid,$2::uuid) RETURNING id::text`, candidate, c.ID).Scan(&application); err != nil {
		return Draft{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO application_draft_sessions(application_id,secret_hash,expires_at) VALUES($1::uuid,$2,now()+$3::bigint * interval '1 second')`, application, hash, int64(ttl.Seconds())); err != nil {
		return Draft{}, err
	}
	d, err := read(ctx, tx, hash, false)
	if err != nil {
		return Draft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Draft{}, err
	}
	return d, nil
}
func (r *PostgresRepository) Update(ctx context.Context, hash []byte, kind string, input Change) (Draft, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Draft{}, err
	}
	defer tx.Rollback(ctx)
	d, err := read(ctx, tx, hash, true)
	if err != nil {
		return Draft{}, err
	}
	if d.State != "DRAFT" {
		return Draft{}, problem(409, "DRAFT_NOT_EDITABLE", "Ce brouillon ne peut plus être modifié.")
	}
	if d.Version != input.Version {
		return Draft{}, ErrConflict
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM admission_campaigns WHERE id=$1::uuid FOR SHARE`, d.Campaign.ID); err != nil {
		return Draft{}, err
	}
	rules := references.NewService(references.NewRepository(tx))
	if _, err = rules.Campaign(ctx, d.Campaign.Code); err != nil {
		return Draft{}, ErrClosed
	}
	switch kind {
	case "profile":
		if input.Profile == nil {
			return Draft{}, problem(400, "INVALID_PARAMETERS", "Le profil est obligatoire.")
		}
		profile, err := NormalizeProfile(*input.Profile)
		if err != nil {
			return Draft{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE candidates SET last_name=$2,first_names=$3,email=$4,phone=$5,updated_at=now() WHERE id=(SELECT candidate_id FROM applications WHERE id=$1::uuid)`, d.ID, profile.LastName, profile.FirstNames, profile.Email, profile.Phone); err != nil {
			return Draft{}, err
		}
	case "academic-background":
		if input.GraduationYear == nil || *input.GraduationYear < 1900 || *input.GraduationYear > time.Now().UTC().Year() {
			return Draft{}, &Problem{Code: "VALIDATION_FAILED", Message: "Vérifiez votre année d’obtention.", Fields: map[string]string{"graduationYear": "L’année doit être comprise entre 1900 et l’année courante."}, Status: 422}
		}
		if _, err = references.NewRepository(tx).Qualification(ctx, input.Qualification); err != nil {
			return Draft{}, problem(422, "QUALIFICATION_INVALID", "Choisissez une qualification reconnue.")
		}
		keep := false
		if d.Program != nil {
			resolved, err := rules.Resolve(ctx, d.Campaign.Code, d.Program.Code, input.Qualification)
			if err != nil {
				return Draft{}, err
			}
			keep = resolved.Eligible
		}
		if _, err = tx.Exec(ctx, `UPDATE applications SET origin_qualification_id=(SELECT id FROM origin_qualifications WHERE code=$2),graduation_year=$3,campaign_program_id=CASE WHEN $4 THEN campaign_program_id ELSE NULL END WHERE id=$1::uuid`, d.ID, input.Qualification, *input.GraduationYear, keep); err != nil {
			return Draft{}, err
		}
	case "program":
		if d.Qualification == nil {
			return Draft{}, problem(422, "ACADEMIC_BACKGROUND_REQUIRED", "Renseignez d’abord votre qualification.")
		}
		resolved, err := rules.Resolve(ctx, d.Campaign.Code, input.Program, d.Qualification.Code)
		if errors.Is(err, references.ErrProgramNotFound) {
			return Draft{}, ErrIneligible
		}
		if err != nil {
			return Draft{}, err
		}
		if !resolved.Eligible {
			return Draft{}, ErrIneligible
		}
		if _, err = tx.Exec(ctx, `UPDATE applications SET campaign_program_id=(SELECT cp.id FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id WHERE cp.campaign_id=$2::uuid AND p.code=$3) WHERE id=$1::uuid`, d.ID, d.Campaign.ID, input.Program); err != nil {
			return Draft{}, err
		}
	default:
		return Draft{}, problem(400, "INVALID_PARAMETERS", "Modification inconnue.")
	}
	if _, err = tx.Exec(ctx, `UPDATE applications SET version=version+1,updated_at=now() WHERE id=$1::uuid`, d.ID); err != nil {
		return Draft{}, err
	}
	d, err = read(ctx, tx, hash, false)
	if err != nil {
		return Draft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Draft{}, err
	}
	return d, nil
}
