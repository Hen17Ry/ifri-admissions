package selection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/platform/storage"
)

// ExportStream carries PDF jobs to the worker (identifier only: the payload, which holds names,
// is fetched over the private network and never goes through Redis).
const ExportStream = "ifri:exports:v1"

const (
	VariantPublic   = "PUBLIC"
	VariantInternal = "INTERNAL"
	maxExportBytes  = 10 << 20
)

func (s *Service) stream() string {
	if s.Stream != "" {
		return s.Stream
	}
	return ExportStream
}

// Document is the content of an export: Go decides every cell; the worker only lays it out.
type Document struct {
	ContractVersion int         `json:"contractVersion"`
	Variant         string      `json:"variant"`
	Title           string      `json:"title"`
	Header          [][2]string `json:"header"`
	Columns         []string    `json:"columns"`
	Rows            [][]string  `json:"rows"`
	Notes           []string    `json:"notes"`
	Landscape       bool        `json:"landscape"`
	Filename        string      `json:"-"`
}

func variantOf(value string) (string, bool) {
	switch strings.ToLower(value) {
	case "public", "publique":
		return VariantPublic, true
	case "internal", "interne":
		return VariantInternal, true
	}
	return "", false
}

// document builds the public or internal list of a frozen ranking at the view's list version.
// The public list holds only admitted and waiting candidates, with their frozen rank and current
// decision: never an average, a criterion, an email or a phone number.
func document(v View, variant string, generated time.Time) Document {
	f := v.Freeze
	version := fmt.Sprintf("Version %d du %s", f.ListVersion, frenchDate(f.VersionAt))
	if f.ListVersion > 1 {
		version += fmt.Sprintf(" (gel du %s)", frenchDate(f.FrozenAt))
	} else {
		version += " (gel)"
	}
	d := Document{ContractVersion: 1, Variant: variant, Header: [][2]string{
		{"Filière", fmt.Sprintf("%s (%s)", v.Offer.ProgramName, v.Offer.ProgramCode)},
		{"Campagne", fmt.Sprintf("%s (%s)", v.Offer.CampaignName, v.Offer.CampaignCode)},
		{"Liste", version},
		{"Généré le", frenchDate(generated) + " (heure du Bénin)"},
	}}
	slug := "publique"
	entries := append([]Entry{}, v.Entries...)
	if variant == VariantPublic {
		d.Title = "Liste des candidats admis et en liste d’attente"
		d.Header = append(d.Header, [2]string{"Diffusion", "Publique"})
		d.Columns = []string{"Rang", "Numéro", "Nom", "Prénoms", "Décision"}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Position < entries[j].Position })
		for _, e := range entries {
			if e.Rank == nil || (e.Decision != Admitted && e.Decision != Waitlisted) {
				continue
			}
			d.Rows = append(d.Rows, []string{strconv.Itoa(*e.Rank), e.Number, e.LastName, e.FirstNames, decisionLabels[e.Decision]})
		}
		d.Notes = []string{"Rang au classement gelé ; décision à la date de cette version de la liste."}
	} else {
		slug = "interne"
		minimum := "aucune"
		if f.MinimumAverage != "" {
			minimum = frenchNumber(f.MinimumAverage)
		}
		d.Title = "Classement et sélection — document interne"
		d.Landscape = true
		d.Header = append(d.Header, [2]string{"Places", strconv.Itoa(f.Seats)}, [2]string{"Moyenne minimale", minimum},
			[2]string{"Diffusion", "Interne — ne pas afficher"})
		d.Columns = []string{"Position", "Rang", "Numéro", "Nom", "Prénoms", "Série", "Moyenne", "Mathématiques", "Départage des mathématiques",
			"Soumis le", "Critère décisif", "Décision au gel", "Décision actuelle"}
		for _, e := range entries {
			rank, math := "", ""
			if e.Rank != nil {
				rank = strconv.Itoa(*e.Rank)
			}
			if e.MathGrade != "" {
				math = frenchNumber(e.MathGrade)
			}
			criterion := criterionLabels[e.DecisiveCriterion]
			if criterion == "" {
				criterion = "Premier de la liste"
			}
			d.Rows = append(d.Rows, []string{strconv.Itoa(e.Position), rank, e.Number, e.LastName, e.FirstNames, e.Qualification,
				frenchNumber(e.Average.Display), math, mathLabels[e.MathTiebreak], frenchDate(e.SubmittedAt), criterion,
				decisionLabels[e.InitialDecision], decisionLabels[e.Decision]})
		}
		d.Notes = []string{
			"Moyenne effective (après corrections approuvées) tronquée à 2 décimales pour la lecture ; le classement compare les fractions exactes.",
			"Départage : moyenne, puis note de mathématiques si tous les candidats à égalité exacte en ont une, puis date de soumission, puis numéro.",
			"Non classé : moyenne effective inférieure à la moyenne minimale.",
		}
	}
	d.Filename = fmt.Sprintf("liste-%s-%s-v%d-%s", strings.ToLower(v.Offer.ProgramCode), strings.ToLower(v.Offer.CampaignCode), f.ListVersion, slug)
	return d
}

// spreadsheetCell neutralises formula injection: a cell that a spreadsheet would read as a
// formula is prefixed with an apostrophe, its content left unchanged.
func spreadsheetCell(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

// CSV renders a document for spreadsheets: UTF-8 with BOM, semicolons, CRLF, header lines first.
func (d Document) CSV() []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // UTF-8 byte order mark: spreadsheets read accents
	w := csv.NewWriter(&buf)
	w.Comma, w.UseCRLF = ';', true
	clean := func(cells []string) []string {
		out := make([]string, len(cells))
		for i, c := range cells {
			out[i] = spreadsheetCell(c)
		}
		return out
	}
	_ = w.Write(clean([]string{d.Title}))
	for _, h := range d.Header {
		_ = w.Write(clean([]string{h[0], h[1]}))
	}
	_ = w.Write([]string{""})
	_ = w.Write(clean(d.Columns))
	for _, r := range d.Rows {
		_ = w.Write(clean(r))
	}
	_ = w.Write([]string{""})
	for _, n := range d.Notes {
		_ = w.Write(clean([]string{n}))
	}
	w.Flush()
	return buf.Bytes()
}

// frozenView loads a frozen offer for an export right.
func (s *Service) frozenView(ctx context.Context, tx pgx.Tx, reader admin.Principal, offer string, upTo int) (View, error) {
	o, err := s.offer(ctx, tx, reader, offer, admin.PermSelectionExport, false)
	if err != nil {
		return View{}, err
	}
	if !o.Frozen {
		return View{}, ErrNotFrozen
	}
	return s.frozen(ctx, tx, o, upTo)
}

// CSV generates the current list of a frozen ranking; each export is audited.
func (s *Service) CSV(ctx context.Context, reader admin.Principal, offer, variant string) (Document, []byte, error) {
	kind, ok := variantOf(variant)
	if !ok {
		return Document{}, nil, admin.NewProblem(400, "INVALID_PARAMETERS", "Variante d’export inconnue.")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Document{}, nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	v, err := s.frozenView(ctx, tx, reader, offer, 0)
	if err != nil {
		return Document{}, nil, err
	}
	d := document(v, kind, now())
	if err = record(ctx, tx, reader.UserID, "SELECTION_EXPORTED", v.Offer.ID, map[string]any{"format": "CSV", "variant": kind, "listVersion": v.Freeze.ListVersion, "rows": len(d.Rows)}); err != nil {
		return Document{}, nil, err
	}
	return d, d.CSV(), tx.Commit(ctx)
}

func (s *Service) exports(ctx context.Context, tx pgx.Tx, offer string) ([]ExportView, error) {
	rows, err := tx.Query(ctx, `SELECT x.id::text,x.list_version,x.variant,x.status,coalesce(x.failure_code,''),x.requested_by::text,u.display_name,x.requested_at
	 FROM selection_exports x JOIN admin_users u ON u.id=x.requested_by WHERE x.campaign_program_id=$1::uuid ORDER BY x.requested_at DESC LIMIT 10`, offer)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ExportView, error) {
		var x ExportView
		return x, row.Scan(&x.ID, &x.ListVersion, &x.Variant, &x.Status, &x.FailureCode, &x.RequestedBy.ID, &x.RequestedBy.Name, &x.RequestedAt)
	})
}

// RequestPDF queues the rendering of the current list by the worker; audited.
func (s *Service) RequestPDF(ctx context.Context, reader admin.Principal, offer, variant string) (ExportView, error) {
	kind, ok := variantOf(variant)
	if !ok {
		return ExportView{}, invalid(map[string]string{"variant": "Choisissez la liste publique ou interne."})
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ExportView{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	v, err := s.frozenView(ctx, tx, reader, offer, 0)
	if err != nil {
		return ExportView{}, err
	}
	x := ExportView{ListVersion: v.Freeze.ListVersion, Variant: kind, Status: "PENDING", RequestedBy: Person{ID: reader.UserID, Name: reader.DisplayName}}
	if err = tx.QueryRow(ctx, `INSERT INTO selection_exports(campaign_program_id,list_version,variant,requested_by) VALUES($1::uuid,$2,$3,$4::uuid) RETURNING id::text,requested_at`,
		v.Offer.ID, x.ListVersion, kind, reader.UserID).Scan(&x.ID, &x.RequestedAt); err != nil {
		return ExportView{}, err
	}
	if err = record(ctx, tx, reader.UserID, "SELECTION_EXPORTED", v.Offer.ID, map[string]any{"format": "PDF", "variant": kind, "listVersion": x.ListVersion, "exportId": x.ID}); err != nil {
		return ExportView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExportView{}, err
	}
	if err = s.dispatch(ctx, x.ID); err != nil {
		s.Logger.Error("export dispatch failed; the background loop will retry", "export_id", x.ID)
	}
	return x, nil
}

type exportRow struct {
	ExportView
	offer, key, sha string
}

func (s *Service) exportRow(ctx context.Context, q pgx.Tx, reader admin.Principal, id string) (exportRow, error) {
	var x exportRow
	if !validUUID(id) {
		return x, ErrExportNotFound
	}
	err := q.QueryRow(ctx, `SELECT x.id::text,x.campaign_program_id::text,x.list_version,x.variant,x.status,coalesce(x.failure_code,''),x.requested_by::text,u.display_name,
	 x.requested_at,coalesce(x.storage_key,''),coalesce(x.sha256,'') FROM selection_exports x JOIN admin_users u ON u.id=x.requested_by WHERE x.id=$1::uuid`, id).
		Scan(&x.ID, &x.offer, &x.ListVersion, &x.Variant, &x.Status, &x.FailureCode, &x.RequestedBy.ID, &x.RequestedBy.Name, &x.RequestedAt, &x.key, &x.sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, ErrExportNotFound
	}
	if err != nil {
		return x, err
	}
	if _, err = s.offer(ctx, q, reader, x.offer, admin.PermSelectionExport, false); err != nil {
		return x, ErrExportNotFound
	}
	return x, nil
}

func (s *Service) ExportState(ctx context.Context, reader admin.Principal, id string) (ExportView, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ExportView{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	x, err := s.exportRow(ctx, tx, reader, id)
	return x.ExportView, err
}

// ExportContent streams a ready PDF; each download is audited.
func (s *Service) ExportContent(ctx context.Context, reader admin.Principal, id string) (storage.Object, string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return storage.Object{}, "", err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	x, err := s.exportRow(ctx, tx, reader, id)
	if err != nil {
		return storage.Object{}, "", err
	}
	if x.Status != "READY" {
		return storage.Object{}, "", ErrExportNotReady
	}
	v, err := s.frozenView(ctx, tx, reader, x.offer, x.ListVersion)
	if err != nil {
		return storage.Object{}, "", err
	}
	if err = audit.RecordAccess(ctx, tx, reader.UserID, "CAMPAIGN_PROGRAM", x.offer, "SELECTION_EXPORT:"+x.Variant); err != nil {
		return storage.Object{}, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return storage.Object{}, "", err
	}
	obj, err := s.Objects.Get(ctx, x.key)
	if err != nil || obj.ContentType != "application/pdf" {
		if err == nil {
			obj.Body.Close()
		}
		return storage.Object{}, "", ErrStorage
	}
	return obj, document(v, x.Variant, x.RequestedAt).Filename + ".pdf", nil
}

func (s *Service) dispatch(ctx context.Context, id string) error {
	body, _ := json.Marshal(map[string]any{"contractVersion": 1, "exportId": id})
	if err := s.Cache.XAdd(ctx, &redis.XAddArgs{Stream: s.stream(), MaxLen: 1000, Approx: true, Values: map[string]any{"job": string(body)}}).Err(); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE selection_exports SET dispatched_at=now() WHERE id=$1::uuid AND status='PENDING'`, id)
	return err
}

// DispatchExports re-sends jobs lost in transit and fails those that never complete.
func (s *Service) DispatchExports(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.redispatch(ctx); err != nil {
				s.Logger.Error("export dispatch unavailable")
			}
		}
	}
}

func (s *Service) redispatch(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `UPDATE selection_exports SET status='FAILED',failure_code='RENDER_FAILED',completed_at=now()
	 WHERE status='PENDING' AND requested_at<now()-interval '10 minutes'`); err != nil {
		return err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id::text FROM selection_exports WHERE status='PENDING' AND (dispatched_at IS NULL OR dispatched_at<now()-interval '2 minutes') ORDER BY requested_at LIMIT 20`)
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

// RegisterInternal mounts the worker's export routes on the private listener (bearer secret).
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
	m.HandleFunc("GET /internal/exports/{id}/payload", auth(s.exportPayload))
	m.HandleFunc("POST /internal/exports/{id}/document", auth(s.exportUpload))
	m.HandleFunc("POST /internal/exports/{id}/failure", auth(s.exportFailure))
}

// exportPayload is the document of a pending export, rebuilt at its list version.
func (s *Service) exportPayload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id := r.PathValue("id")
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		w.WriteHeader(503)
		return
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var offer, variant string
	var version int
	var requested time.Time
	if !validUUID(id) || tx.QueryRow(ctx, `SELECT campaign_program_id::text,variant,list_version,requested_at FROM selection_exports WHERE id=$1::uuid AND status='PENDING'`, id).
		Scan(&offer, &variant, &version, &requested) != nil {
		w.WriteHeader(404)
		return
	}
	o, _, err := scanOffer(tx.QueryRow(ctx, offerSQL+` WHERE cp.id=$1::uuid`, offer))
	if err != nil {
		w.WriteHeader(503)
		return
	}
	v, err := s.frozen(ctx, tx, o, version)
	if err != nil {
		w.WriteHeader(503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(document(v, variant, requested))
}

// exportUpload accepts only a bounded PDF for a pending export, stores it privately, then READY.
func (s *Service) exportUpload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id := r.PathValue("id")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxExportBytes))
	if err != nil || !validUUID(id) || r.Header.Get("Content-Type") != "application/pdf" {
		w.WriteHeader(400)
		return
	}
	tail := body[max(0, len(body)-1024):]
	if !bytes.HasPrefix(body, []byte("%PDF-")) || !bytes.Contains(tail, []byte("%%EOF")) {
		_, _ = s.Pool.Exec(ctx, `UPDATE selection_exports SET status='FAILED',failure_code='DOCUMENT_REJECTED',completed_at=now() WHERE id=$1::uuid AND status='PENDING'`, id)
		w.WriteHeader(422)
		return
	}
	var offer string
	if err = s.Pool.QueryRow(ctx, `SELECT campaign_program_id::text FROM selection_exports WHERE id=$1::uuid AND status='PENDING'`, id).Scan(&offer); err != nil {
		w.WriteHeader(409)
		return
	}
	sum := sha256.Sum256(body)
	key := fmt.Sprintf("selection/%s/exports/%s.pdf", offer, id)
	if err = s.Objects.PutContent(ctx, key, bytes.NewReader(body), int64(len(body)), "application/pdf"); err != nil {
		// Transient or policy failure: the worker keeps the job and retries until expiry.
		s.Logger.Error("selection export storage failed", "export_id", id)
		w.WriteHeader(503)
		return
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE selection_exports SET status='READY',storage_key=$2,sha256=$3,size_bytes=$4,completed_at=now() WHERE id=$1::uuid AND status='PENDING'`,
		id, key, hex.EncodeToString(sum[:]), len(body)); err != nil {
		w.WriteHeader(503)
		return
	}
	s.Logger.Info("selection export stored", "export_id", id, "size_bytes", len(body))
	w.WriteHeader(204)
}

func (s *Service) exportFailure(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"failureCode"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || input.Code != "RENDER_FAILED" || !validUUID(r.PathValue("id")) {
		w.WriteHeader(400)
		return
	}
	if _, err := s.Pool.Exec(r.Context(), `UPDATE selection_exports SET status='FAILED',failure_code=$2,completed_at=now() WHERE id=$1::uuid AND status='PENDING'`, r.PathValue("id"), input.Code); err != nil {
		w.WriteHeader(503)
		return
	}
	s.Logger.Info("selection export failed", "export_id", r.PathValue("id"))
	w.WriteHeader(204)
}
