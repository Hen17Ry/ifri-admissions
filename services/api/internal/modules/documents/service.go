package documents

import (
	"context"
	"errors"
	"fmt"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/platform/pdfcheck"
	"ifri-admissions/services/api/internal/platform/storage"
	"io"
	"log/slog"
	"time"
)

type Service struct {
	repo      Repository
	objects   storage.Objects
	inspector pdfcheck.Inspector
	max       int64
	logger    *slog.Logger
	slots     chan struct{}
}

func NewService(repo Repository, objects storage.Objects, inspector pdfcheck.Inspector, max int64, logger *slog.Logger) *Service {
	return &Service{repo, objects, inspector, max, logger, make(chan struct{}, 2)}
}
func (s *Service) List(ctx context.Context, hash []byte) (Listing, error) {
	d, err := s.repo.Draft(ctx, hash)
	if err != nil {
		return Listing{}, err
	}
	current, err := s.repo.List(ctx, d.ID)
	if err != nil {
		return Listing{}, err
	}
	out := Listing{Documents: []Document{}, Complete: true, MaxBytes: s.max, ApplicationVersion: d.Version}
	for _, t := range Types {
		doc := Document{Type: t}
		for _, found := range current {
			if found.Code == t.Code {
				found.Type = t
				doc = found
				break
			}
		}
		out.Documents = append(out.Documents, doc)
		if t.Required && !doc.Uploaded {
			out.Complete = false
		}
	}
	return out, nil
}
func (s *Service) authorize(ctx context.Context, hash []byte, version int64, code string) (drafts.Draft, error) {
	if !Known(code) {
		return drafts.Draft{}, issue(400, "DOCUMENT_TYPE_INVALID", "Ce type de document n’est pas reconnu.")
	}
	d, err := s.repo.Draft(ctx, hash)
	if err != nil {
		return d, err
	}
	if d.State != "DRAFT" {
		return d, issue(409, "DRAFT_NOT_EDITABLE", "Ce dossier ne peut plus être modifié.")
	}
	if d.Version != version {
		return d, drafts.ErrConflict
	}
	now := time.Now()
	if d.Campaign.Status != "OPEN" || now.Before(d.Campaign.StartsAt) || !now.Before(d.Campaign.EndsAt) {
		return d, drafts.ErrClosed
	}
	return d, nil
}
func (s *Service) Upload(ctx context.Context, hash []byte, applicationVersion int64, code, filename, mime string, body io.Reader) (Listing, error) {
	if _, err := s.authorize(ctx, hash, applicationVersion, code); err != nil {
		return Listing{}, err
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return Listing{}, issue(408, "UPLOAD_INTERRUPTED", "L’envoi a été interrompu. Réessayez.")
	}
	upload, err := Validate(ctx, body, filename, mime, s.max, s.inspector)
	if err != nil {
		return Listing{}, err
	}
	defer upload.Close()
	return s.uploadValidated(ctx, hash, applicationVersion, code, upload)
}
func (s *Service) uploadValidated(ctx context.Context, hash []byte, applicationVersion int64, code string, upload Upload) (Listing, error) {
	d, err := s.authorize(ctx, hash, applicationVersion, code)
	if err != nil {
		return Listing{}, err
	}
	id, err := newUUID()
	if err != nil {
		return Listing{}, err
	}
	key := fmt.Sprintf("applications/%s/documents/%s/%s.pdf", d.ID, code, id)
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.objects.Delete(ctx, key); err != nil {
			s.logger.Error("document cleanup failed", "application_id", d.ID, "document_type", code, "object_id", id)
		}
	}
	if err = s.objects.Put(ctx, key, upload.File, upload.Size); err != nil {
		cleanup()
		return Listing{}, issue(503, "STORAGE_UNAVAILABLE", "Le stockage est momentanément indisponible. Réessayez.")
	}
	err = s.repo.Save(ctx, hash, applicationVersion, code, Version{ID: id, Key: key, Filename: upload.Filename, Size: upload.Size, SHA256: upload.Hash})
	if err != nil {
		if !errors.Is(err, ErrCommitUnknown) {
			cleanup()
		} else {
			s.logger.Error("document commit uncertain; object retained", "application_id", d.ID, "document_type", code, "object_id", id)
		}
		return Listing{}, err
	}
	s.logger.Info("document upload saved", "application_id", d.ID, "document_type", code, "size_bytes", upload.Size)
	return s.List(ctx, hash)
}
func (s *Service) Content(ctx context.Context, hash []byte, code string) (Version, storage.Object, error) {
	if !Known(code) {
		return Version{}, storage.Object{}, issue(400, "DOCUMENT_TYPE_INVALID", "Ce type de document n’est pas reconnu.")
	}
	d, err := s.repo.Draft(ctx, hash)
	if err != nil {
		return Version{}, storage.Object{}, err
	}
	v, err := s.repo.Current(ctx, d.ID, code)
	if err != nil {
		return v, storage.Object{}, err
	}
	obj, err := s.objects.Get(ctx, v.Key)
	if err != nil {
		return v, obj, issue(503, "STORAGE_UNAVAILABLE", "Le document est momentanément indisponible.")
	}
	if obj.Size != v.Size || obj.ContentType != "application/pdf" {
		obj.Body.Close()
		return v, storage.Object{}, issue(503, "DOCUMENT_INTEGRITY_ERROR", "Le document ne peut pas être affiché actuellement.")
	}
	return v, obj, nil
}
