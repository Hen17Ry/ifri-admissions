package documents

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/modules/references"
	"time"
)

var ErrCommitUnknown = errors.New("document commit outcome unknown")

type Repository interface {
	Draft(context.Context, []byte) (drafts.Draft, error)
	List(context.Context, string) ([]Document, error)
	Current(context.Context, string, string) (Version, error)
	Save(context.Context, []byte, int64, string, Version) error
}
type PostgresRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PostgresRepository { return &PostgresRepository{pool} }
func (r *PostgresRepository) Draft(ctx context.Context, hash []byte) (drafts.Draft, error) {
	return drafts.Read(ctx, r.pool, hash, false)
}
func (r *PostgresRepository) List(ctx context.Context, application string) ([]Document, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.document_type,v.version_number,v.original_filename,v.size_bytes,v.created_at FROM application_documents d JOIN document_versions v ON v.id=d.current_version_id WHERE d.application_id=$1::uuid`, application)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Document{}
	for rows.Next() {
		var d Document
		var created time.Time
		if err = rows.Scan(&d.Code, &d.Version, &d.Filename, &d.Size, &created); err != nil {
			return nil, err
		}
		d.Uploaded = true
		d.CreatedAt = &created
		result = append(result, d)
	}
	return result, rows.Err()
}
func (r *PostgresRepository) Current(ctx context.Context, application, code string) (Version, error) {
	var v Version
	err := r.pool.QueryRow(ctx, `SELECT v.id::text,d.id::text,v.storage_key,v.original_filename,v.sha256,v.version_number,v.size_bytes,v.created_at FROM application_documents d JOIN document_versions v ON v.id=d.current_version_id WHERE d.application_id=$1::uuid AND d.document_type=$2`, application, code).Scan(&v.ID, &v.DocumentID, &v.Key, &v.Filename, &v.SHA256, &v.Number, &v.Size, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, issue(404, "DOCUMENT_NOT_FOUND", "Ce document n’a pas encore été ajouté.")
	}
	return v, err
}
func (r *PostgresRepository) Save(ctx context.Context, hash []byte, applicationVersion int64, code string, v Version) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	d, err := drafts.Read(ctx, tx, hash, true)
	if err != nil {
		return err
	}
	if err = drafts.Editable(d); err != nil {
		return err
	}
	if d.Version != applicationVersion {
		return drafts.ErrConflict
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM admission_campaigns WHERE id=$1::uuid FOR SHARE`, d.Campaign.ID); err != nil {
		return err
	}
	if _, err = references.NewService(references.NewRepository(tx)).Campaign(ctx, d.Campaign.Code); err != nil {
		return drafts.ErrClosed
	}
	var docID string
	var old int
	err = tx.QueryRow(ctx, `SELECT d.id::text,v.version_number FROM application_documents d JOIN document_versions v ON v.id=d.current_version_id WHERE d.application_id=$1::uuid AND d.document_type=$2 FOR UPDATE OF d`, d.ID, code).Scan(&docID, &old)
	if errors.Is(err, pgx.ErrNoRows) {
		docID, err = newUUID()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO application_documents(id,application_id,document_type,current_version_id) VALUES($1::uuid,$2::uuid,$3,$4::uuid)`, docID, d.ID, code, v.ID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if _, err = tx.Exec(ctx, `UPDATE document_versions SET replaced_at=now() WHERE id=(SELECT current_version_id FROM application_documents WHERE id=$1::uuid)`, docID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO document_versions(id,application_document_id,version_number,storage_key,original_filename,content_type,size_bytes,sha256) VALUES($1::uuid,$2::uuid,$3,$4,$5,'application/pdf',$6,$7)`, v.ID, docID, old+1, v.Key, v.Filename, v.Size, v.SHA256); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE application_documents SET current_version_id=$2::uuid,updated_at=now() WHERE id=$1::uuid`, docID, v.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE applications SET version=version+1,updated_at=now() WHERE id=$1::uuid`, d.ID); err != nil {
		return err
	}
	// Recheck expiry before committing an upload which can take longer than a normal PATCH.
	if _, err = drafts.Read(ctx, tx, hash, false); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) {
			return err
		}
		return ErrCommitUnknown
	}
	return nil
}
