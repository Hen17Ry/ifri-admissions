package selection

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Benin time in exports, whatever the image ships
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/modules/submissions"
	"ifri-admissions/services/api/internal/platform/storage"
)

type Service struct {
	Pool    *pgxpool.Pool
	Objects storage.TypedObjects
	Cache   *redis.Client
	Logger  *slog.Logger
	Stream  string // export stream; tests use their own
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

func text(value string, max int) (string, bool) {
	value = strings.TrimSpace(value)
	return value, value != "" && utf8.RuneCountInString(value) <= max && utf8.ValidString(value)
}

func record(ctx context.Context, tx pgx.Tx, actor, event, offer string, metadata map[string]any) error {
	return audit.Record(ctx, tx, event, audit.Actor{Type: audit.ActorAdmin, ID: actor}, "CAMPAIGN_PROGRAM", offer, metadata)
}

const offerSQL = `SELECT cp.id::text,c.code,c.name,p.code,p.name,p.id::text,cp.available_seats,coalesce(cp.minimum_average::text,''),
 (c.status='OPEN' AND now()>=c.starts_at AND now()<c.ends_at),c.ends_at,ranking_frozen(cp.id)
 FROM campaign_programs cp JOIN admission_campaigns c ON c.id=cp.campaign_id JOIN programs p ON p.id=cp.program_id`

func scanOffer(row pgx.Row) (Offer, string, error) {
	var o Offer
	var program string
	err := row.Scan(&o.ID, &o.CampaignCode, &o.CampaignName, &o.ProgramCode, &o.ProgramName, &program, &o.Seats, &o.MinimumAverage, &o.CampaignOpen, &o.EndsAt, &o.Frozen)
	return o, program, err
}

// Offers lists the programs of every campaign the reader may rank.
func (s *Service) Offers(ctx context.Context, reader admin.Principal) ([]Offer, error) {
	rows, err := s.Pool.Query(ctx, offerSQL+` WHERE cp.enabled AND c.status<>'DRAFT' ORDER BY c.starts_at DESC,p.code`)
	if err != nil {
		return nil, err
	}
	out := []Offer{}
	for rows.Next() {
		o, program, err := scanOffer(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if reader.CanFor(admin.PermRankingRead, program) {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}

// offer loads one offer for an action and checks the reader's right on its program.
func (s *Service) offer(ctx context.Context, q pgx.Tx, reader admin.Principal, id string, perm admin.Permission, lock bool) (Offer, error) {
	if !validUUID(id) {
		return Offer{}, ErrOfferNotFound
	}
	if lock {
		// Lock first, then read with a fresh snapshot: a freeze committed during the wait shows.
		if _, err := q.Exec(ctx, `SELECT 1 FROM campaign_programs WHERE id=$1::uuid FOR UPDATE`, id); err != nil {
			return Offer{}, err
		}
	}
	o, program, err := scanOffer(q.QueryRow(ctx, offerSQL+` WHERE cp.id=$1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !reader.CanFor(perm, program)) {
		return Offer{}, ErrOfferNotFound
	}
	return o, err
}

type candidateRow struct {
	Candidate
	lastName, firstNames, qualification string
	weighted, coefficients, average     string
	exact                               bool
	mathText                            string
}

// candidates reads the validated dossiers of a program with their effective values: the review's
// exact sums and the effective grade (latest approved correction, else the snapshot) of the
// subject whose code is listed in ranking_math_subjects.
func candidates(ctx context.Context, q pgx.Tx, offer string) ([]candidateRow, error) {
	rows, err := q.Query(ctx, `SELECT a.id::text,a.application_number,a.submitted_at,s.last_name,s.first_names,qu.label,
	 r.effective_weighted_sum::text,r.effective_coefficient_sum::text,r.effective_average::text,r.effective_average_is_exact,
	 coalesce(m.n,0),coalesce(m.code,''),coalesce(m.grade::text,'')
	 FROM application_reviews r JOIN applications a ON a.id=r.application_id
	 JOIN application_submissions s ON s.application_id=r.application_id
	 JOIN origin_qualifications qu ON qu.id=s.origin_qualification_id
	 LEFT JOIN LATERAL (
	  SELECT count(*)::int AS n,min(sub.code) AS code,min(coalesce((SELECT k.proposed_value FROM application_corrections k
	   WHERE k.application_id=r.application_id AND k.subject_id=x.subject_id AND k.field='GRADE' AND k.status='APPROVED'
	   ORDER BY k.decided_at DESC,k.id DESC LIMIT 1),x.grade_value)) AS grade
	  FROM application_submission_subjects x JOIN subjects sub ON sub.id=x.subject_id
	  WHERE x.submission_id=s.id AND sub.code IN (SELECT code FROM ranking_math_subjects)) m ON true
	 WHERE r.campaign_program_id=$1::uuid AND r.status='VALIDATED'`, offer)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidateRow, error) {
		var c candidateRow
		if err := row.Scan(&c.ApplicationID, &c.Number, &c.SubmittedAt, &c.lastName, &c.firstNames, &c.qualification,
			&c.weighted, &c.coefficients, &c.average, &c.exact, &c.MathSubjects, &c.MathSubject, &c.mathText); err != nil {
			return c, err
		}
		var err error
		if c.Weighted, err = submissions.ParseNumeric(c.weighted); err != nil {
			return c, err
		}
		if c.Coefficients, err = submissions.ParseNumeric(c.coefficients); err != nil {
			return c, err
		}
		if c.MathSubjects == 1 {
			if c.MathGrade, err = submissions.ParseNumeric(c.mathText); err != nil {
				return c, err
			}
		} else {
			c.MathSubject, c.mathText = "", ""
		}
		return c, nil
	})
}

func (s *Service) counts(ctx context.Context, q pgx.Tx, offer string) (Counts, error) {
	var c Counts
	err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='VALIDATED'),count(*) FILTER (WHERE status='REJECTED'),
	 count(*) FILTER (WHERE status='TO_REVIEW'),count(*) FILTER (WHERE status='IN_REVIEW'),
	 (SELECT count(*) FROM application_corrections k JOIN application_reviews r2 ON r2.application_id=k.application_id
	  WHERE r2.campaign_program_id=$1::uuid AND k.status='PENDING')
	 FROM application_reviews WHERE campaign_program_id=$1::uuid`, offer).Scan(&c.Validated, &c.Rejected, &c.ToReview, &c.InReview, &c.PendingCorrections)
	return c, err
}

// blockers are the unmet preconditions of a freeze, as the database checks them again.
func blockers(o Offer, c Counts, rows []candidateRow) []Blocker {
	out := []Blocker{}
	if o.CampaignOpen {
		out = append(out, Blocker{Code: "CAMPAIGN_OPEN", Message: "La période de candidature n’est pas terminée."})
	}
	if pending := c.ToReview + c.InReview; pending > 0 {
		out = append(out, Blocker{Code: "DOSSIERS_PENDING", Count: pending, Message: plural(pending, "dossier n’est pas encore validé ou rejeté.", "dossiers ne sont pas encore validés ou rejetés.")})
	}
	if c.PendingCorrections > 0 {
		out = append(out, Blocker{Code: "CORRECTIONS_PENDING", Count: c.PendingCorrections, Message: plural(c.PendingCorrections, "correction attend la décision du directeur adjoint.", "corrections attendent la décision du directeur adjoint.")})
	}
	if o.Seats == nil {
		out = append(out, Blocker{Code: "SEATS_MISSING", Message: "Le nombre de places n’est pas défini."})
	}
	ambiguous := 0
	for _, r := range rows {
		if r.MathSubjects > 1 {
			ambiguous++
		}
	}
	if ambiguous > 0 {
		out = append(out, Blocker{Code: "MATH_AMBIGUOUS", Count: ambiguous, Message: plural(ambiguous, "dossier a plusieurs matières mathématiques dans sa grille : le départage est indéterminé.", "dossiers ont plusieurs matières mathématiques dans leur grille : le départage est indéterminé.")})
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func parseMinimum(value string) (*big.Rat, error) {
	if value == "" {
		return nil, nil
	}
	return submissions.ParseNumeric(value)
}

func entryOf(c candidateRow, r Ranked) Entry {
	e := Entry{ApplicationID: c.ApplicationID, Number: c.Number, LastName: c.lastName, FirstNames: c.firstNames, Qualification: c.qualification,
		Position: r.Position, MathSubject: c.MathSubject, MathGrade: c.mathText, MathTiebreak: r.MathTiebreak, SubmittedAt: c.SubmittedAt,
		DecisiveCriterion: r.Decisive, InitialDecision: r.Decision, Decision: r.Decision,
		Average: Average{Display: AverageDisplay(c.Weighted, c.Coefficients), Decimal: c.average, Exact: c.exact, WeightedSum: c.weighted, CoefficientSum: c.coefficients}}
	if r.Rank > 0 {
		rank := r.Rank
		e.Rank = &rank
	}
	return e
}

// provisional computes the ranking on demand from the validated dossiers.
func (s *Service) provisional(ctx context.Context, tx pgx.Tx, o Offer) (View, []candidateRow, []Ranked, error) {
	v := View{Offer: o, Entries: []Entry{}, Events: []EventView{}, Exports: []ExportView{}}
	var err error
	if v.Counts, err = s.counts(ctx, tx, o.ID); err != nil {
		return v, nil, nil, err
	}
	rows, err := candidates(ctx, tx, o.ID)
	if err != nil {
		return v, nil, nil, err
	}
	minimum, err := parseMinimum(o.MinimumAverage)
	if err != nil {
		return v, nil, nil, err
	}
	list := make([]Candidate, len(rows))
	byID := map[string]candidateRow{}
	for i, r := range rows {
		list[i] = r.Candidate
		byID[r.ApplicationID] = r
	}
	ranked := Rank(list, o.Seats, minimum)
	for _, r := range ranked {
		e := entryOf(byID[r.ApplicationID], r)
		e.InitialDecision = ""
		v.Entries = append(v.Entries, e)
	}
	v.Blockers = blockers(o, v.Counts, rows)
	return v, rows, ranked, nil
}

// View returns the provisional ranking, or the frozen one with the current decisions; the
// consultation of this personal data is audited.
func (s *Service) View(ctx context.Context, reader admin.Principal, offer string) (View, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := s.offer(ctx, tx, reader, offer, admin.PermRankingRead, false)
	if err != nil {
		return View{}, err
	}
	var v View
	if o.Frozen {
		v, err = s.frozen(ctx, tx, o, 0)
	} else {
		v, _, _, err = s.provisional(ctx, tx, o)
	}
	if err != nil {
		return View{}, err
	}
	if err = audit.RecordAccess(ctx, tx, reader.UserID, "CAMPAIGN_PROGRAM", o.ID, "RANKING"); err != nil {
		return View{}, err
	}
	return v, tx.Commit(ctx)
}

// frozen reads the snapshot and the events up to a list version (0: the current one).
func (s *Service) frozen(ctx context.Context, tx pgx.Tx, o Offer, upTo int) (View, error) {
	v := View{Offer: o, Blockers: []Blocker{}, Entries: []Entry{}, Events: []EventView{}, Exports: []ExportView{}}
	var err error
	if v.Counts, err = s.counts(ctx, tx, o.ID); err != nil {
		return v, err
	}
	f := &Freeze{}
	if err = tx.QueryRow(ctx, `SELECT f.frozen_at,f.frozen_by::text,u.display_name,f.available_seats,coalesce(f.minimum_average::text,'')
	 FROM ranking_freezes f JOIN admin_users u ON u.id=f.frozen_by WHERE f.campaign_program_id=$1::uuid`, o.ID).
		Scan(&f.FrozenAt, &f.FrozenBy.ID, &f.FrozenBy.Name, &f.Seats, &f.MinimumAverage); err != nil {
		return v, err
	}
	rows, err := tx.Query(ctx, `SELECT e.application_id::text,e.application_number,s.last_name,s.first_names,qu.label,e.position,coalesce(e.rank,0),
	 e.weighted_sum::text,e.coefficient_sum::text,e.average::text,e.average_is_exact,coalesce(e.math_subject_code,''),coalesce(e.math_grade::text,''),
	 e.math_tiebreak,e.submitted_at,coalesce(e.decisive_criterion,''),e.initial_decision
	 FROM ranking_entries e JOIN application_submissions s ON s.application_id=e.application_id
	 JOIN origin_qualifications qu ON qu.id=s.origin_qualification_id
	 WHERE e.campaign_program_id=$1::uuid ORDER BY e.position`, o.ID)
	if err != nil {
		return v, err
	}
	var entries []Ranked
	for rows.Next() {
		var e Entry
		var rank int
		if err = rows.Scan(&e.ApplicationID, &e.Number, &e.LastName, &e.FirstNames, &e.Qualification, &e.Position, &rank,
			&e.Average.WeightedSum, &e.Average.CoefficientSum, &e.Average.Decimal, &e.Average.Exact, &e.MathSubject, &e.MathGrade,
			&e.MathTiebreak, &e.SubmittedAt, &e.DecisiveCriterion, &e.InitialDecision); err != nil {
			rows.Close()
			return v, err
		}
		w, _ := submissions.ParseNumeric(e.Average.WeightedSum)
		c, _ := submissions.ParseNumeric(e.Average.CoefficientSum)
		if w != nil && c != nil && c.Sign() > 0 {
			e.Average.Display = AverageDisplay(w, c)
		}
		if rank > 0 {
			r := rank
			e.Rank = &r
		}
		v.Entries = append(v.Entries, e)
		entries = append(entries, Ranked{Candidate: Candidate{ApplicationID: e.ApplicationID}, Rank: rank, Decision: e.InitialDecision})
	}
	if err = rows.Err(); err != nil {
		return v, err
	}
	events, views, err := s.events(ctx, tx, o.ID)
	if err != nil {
		return v, err
	}
	f.ListVersion, f.VersionAt = 1, f.FrozenAt
	if upTo == 0 {
		upTo = 1 + len(events)
	}
	for _, ev := range views {
		if ev.Version <= upTo {
			v.Events = append(v.Events, ev)
			f.ListVersion, f.VersionAt = ev.Version, ev.OccurredAt
		}
	}
	states := States(entries, events, upTo)
	for i := range v.Entries {
		v.Entries[i].Decision = states[v.Entries[i].ApplicationID]
	}
	f.FreeSeats = FreeSeats(f.Seats, states)
	v.Freeze = f
	if first, ok := FirstWaiting(entries, states); ok && f.FreeSeats > 0 {
		for i := range v.Entries {
			if v.Entries[i].ApplicationID == first.ApplicationID {
				proposal := v.Entries[i]
				v.Proposal = &proposal
			}
		}
	}
	if v.Exports, err = s.exports(ctx, tx, o.ID); err != nil {
		return v, err
	}
	return v, nil
}

func (s *Service) events(ctx context.Context, tx pgx.Tx, offer string) ([]Event, []EventView, error) {
	rows, err := tx.Query(ctx, `SELECT ev.list_version,ev.event_type,ev.application_id::text,e.application_number,s.last_name||' '||s.first_names,
	 coalesce(ev.reason,''),ev.actor_id::text,u.display_name,ev.occurred_at
	 FROM selection_events ev JOIN ranking_entries e ON e.campaign_program_id=ev.campaign_program_id AND e.application_id=ev.application_id
	 JOIN application_submissions s ON s.application_id=ev.application_id JOIN admin_users u ON u.id=ev.actor_id
	 WHERE ev.campaign_program_id=$1::uuid ORDER BY ev.list_version`, offer)
	if err != nil {
		return nil, nil, err
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (EventView, error) {
		var e EventView
		return e, row.Scan(&e.Version, &e.Type, &e.ApplicationID, &e.Number, &e.Candidate, &e.Reason, &e.Actor.ID, &e.Actor.Name, &e.OccurredAt)
	})
	if err != nil {
		return nil, nil, err
	}
	events := make([]Event, len(views))
	for i, e := range views {
		events[i] = Event{Version: e.Version, Type: e.Type, ApplicationID: e.ApplicationID}
	}
	return events, views, nil
}

// Settings are the Director's seats (1 to 10,000, or unset) and optional minimum average.
type Settings struct {
	Seats          *int   `json:"seats"`
	MinimumAverage string `json:"minimumAverage"`
}

var minimumPattern = regexp.MustCompile(`^[0-9]{1,2}([.,][0-9]{1,2})?$`)

func (in Settings) validate() (string, error) {
	fields := map[string]string{}
	if in.Seats != nil && (*in.Seats < 1 || *in.Seats > 10000) {
		fields["seats"] = "Indiquez un nombre de places entre 1 et 10 000."
	}
	minimum := strings.ReplaceAll(strings.TrimSpace(in.MinimumAverage), ",", ".")
	if minimum != "" {
		value, err := submissions.ParseNumeric(minimum)
		if !minimumPattern.MatchString(minimum) || err != nil || value.Cmp(big.NewRat(20, 1)) > 0 {
			fields["minimumAverage"] = "Indiquez une moyenne entre 0 et 20, avec 2 décimales au plus, ou laissez vide."
		}
	}
	if len(fields) > 0 {
		return "", invalid(fields)
	}
	return minimum, nil
}

// SetSettings records the seats and the minimum average until the freeze (audited).
func (s *Service) SetSettings(ctx context.Context, actor admin.Principal, offer string, in Settings) (View, error) {
	minimum, err := in.validate()
	if err != nil {
		return View{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := s.offer(ctx, tx, actor, offer, admin.PermRankingConfigure, true)
	if err != nil {
		return View{}, err
	}
	if o.Frozen {
		return View{}, ErrFrozen
	}
	var after string
	if err = tx.QueryRow(ctx, `UPDATE campaign_programs SET available_seats=$2,minimum_average=nullif($3,'')::numeric WHERE id=$1::uuid
	 RETURNING coalesce(minimum_average::text,'')`, o.ID, in.Seats, minimum).Scan(&after); err != nil {
		return View{}, err
	}
	if err = record(ctx, tx, actor.UserID, "RANKING_SETTINGS_CHANGED", o.ID, map[string]any{
		"seatsBefore": o.Seats, "seatsAfter": in.Seats, "minimumAverageBefore": o.MinimumAverage, "minimumAverageAfter": after}); err != nil {
		return View{}, err
	}
	o.Seats, o.MinimumAverage = in.Seats, after
	v, _, _, err := s.provisional(ctx, tx, o)
	if err != nil {
		return View{}, err
	}
	return v, tx.Commit(ctx)
}

// Freeze validates the ranking of a program: preconditions checked under locks (the campaign
// row waits for in-flight submissions, then the offer, then every review of the program, which
// review and correction writes always lock first), snapshot written, review writes blocked.
func (s *Service) Freeze(ctx context.Context, actor admin.Principal, offer string, in Settings) (View, error) {
	if !validUUID(offer) {
		return View{}, ErrOfferNotFound
	}
	expected, err := in.validate()
	if err != nil {
		return View{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT c.id FROM admission_campaigns c JOIN campaign_programs cp ON cp.campaign_id=c.id WHERE cp.id=$1::uuid FOR UPDATE OF c`, offer); err != nil {
		return View{}, err
	}
	o, err := s.offer(ctx, tx, actor, offer, admin.PermSelectionValidate, true)
	if err != nil {
		return View{}, err
	}
	if o.Frozen {
		return View{}, ErrFrozen
	}
	if (o.Seats == nil) != (in.Seats == nil) || (o.Seats != nil && *o.Seats != *in.Seats) || !sameDecimal(o.MinimumAverage, expected) {
		return View{}, ErrSettingsChanged
	}
	if _, err = tx.Exec(ctx, `SELECT 1 FROM application_reviews WHERE campaign_program_id=$1::uuid ORDER BY application_id FOR UPDATE`, offer); err != nil {
		return View{}, err
	}
	v, rows, ranked, err := s.provisional(ctx, tx, o)
	if err != nil {
		return View{}, err
	}
	if len(v.Blockers) > 0 {
		return View{}, blocked(v.Blockers)
	}
	byID := map[string]candidateRow{}
	for _, r := range rows {
		byID[r.ApplicationID] = r
	}
	rankedCount, admitted := 0, 0
	for _, r := range ranked {
		if r.Rank > 0 {
			rankedCount++
		}
		if r.Decision == Admitted {
			admitted++
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ranking_freezes(campaign_program_id,frozen_by,available_seats,minimum_average,ranked_count,not_ranked_count,admitted_count,calculation_version)
	 VALUES($1::uuid,$2::uuid,$3,nullif($4,'')::numeric,$5,$6,$7,$8)`, offer, actor.UserID, *o.Seats, o.MinimumAverage, rankedCount, len(ranked)-rankedCount, admitted, CalculationVersion); err != nil {
		return View{}, err
	}
	// One row per validated dossier; the guard and the commit check of migration 00009 verify
	// every row and the whole order against the database.
	n := len(ranked)
	apps, numbers, weighted, coefficients, averages, displays, tiebreaks, decisions := make([]string, n), make([]string, n), make([]string, n),
		make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	positions, exact, dates := make([]int, n), make([]bool, n), make([]time.Time, n)
	ranks := make([]*int, n)
	codes, grades, criteria := make([]*string, n), make([]*string, n), make([]*string, n)
	for i, r := range ranked {
		c := byID[r.ApplicationID]
		apps[i], numbers[i], positions[i], dates[i] = r.ApplicationID, c.Number, r.Position, c.SubmittedAt
		weighted[i], coefficients[i], averages[i], exact[i] = c.weighted, c.coefficients, c.average, c.exact
		displays[i], tiebreaks[i], decisions[i] = AverageDisplay(c.Weighted, c.Coefficients), r.MathTiebreak, r.Decision
		if r.Rank > 0 {
			rank := r.Rank
			ranks[i] = &rank
		}
		if r.Decisive != "" {
			criterion := r.Decisive
			criteria[i] = &criterion
		}
		if c.MathSubject != "" {
			code, grade := c.MathSubject, c.mathText
			codes[i], grades[i] = &code, &grade
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ranking_entries(campaign_program_id,application_id,position,rank,weighted_sum,coefficient_sum,average,average_is_exact,
	  average_display,math_subject_code,math_grade,math_tiebreak,submitted_at,application_number,decisive_criterion,initial_decision)
	 SELECT $1::uuid,u.app,u.pos,u.rank,u.w::numeric,u.c::numeric,u.avg::numeric,u.exact,u.display,u.code,u.grade::numeric,u.tiebreak,u.at,u.number,u.criterion,u.decision
	 FROM unnest($2::uuid[],$3::int[],$4::int[],$5::text[],$6::text[],$7::text[],$8::bool[],$9::text[],$10::text[],$11::text[],$12::text[],$13::timestamptz[],$14::text[],$15::text[],$16::text[])
	  AS u(app,pos,rank,w,c,avg,exact,display,code,grade,tiebreak,at,number,criterion,decision) ORDER BY u.pos`,
		offer, apps, positions, ranks, weighted, coefficients, averages, exact, displays, codes, grades, tiebreaks, dates, numbers, criteria, decisions); err != nil {
		return View{}, err
	}
	if err = record(ctx, tx, actor.UserID, "RANKING_FROZEN", offer, map[string]any{"seats": *o.Seats, "minimumAverage": o.MinimumAverage,
		"ranked": rankedCount, "notRanked": len(ranked) - rankedCount, "admitted": admitted, "calculationVersion": CalculationVersion}); err != nil {
		return View{}, err
	}
	o.Frozen = true
	frozen, err := s.frozen(ctx, tx, o, 0)
	if err != nil {
		return View{}, err
	}
	return frozen, tx.Commit(ctx)
}

func sameDecimal(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	x, err1 := submissions.ParseNumeric(a)
	y, err2 := submissions.ParseNumeric(b)
	return err1 == nil && err2 == nil && x.Cmp(y) == 0
}

// Move is a withdrawal (reason required) or a promotion; ListVersion is the version the deputy
// director saw, so that two simultaneous decisions never both apply.
type Move struct {
	ApplicationID string `json:"applicationId"`
	Reason        string `json:"reason,omitempty"`
	ListVersion   int    `json:"listVersion"`
}

// Record applies one selection event after the freeze, under the freeze row lock.
func (s *Service) Record(ctx context.Context, actor admin.Principal, offer, kind string, in Move) (View, error) {
	reason, hasReason := text(in.Reason, 2000)
	if kind == "WITHDRAWN" && !hasReason {
		return View{}, invalid(map[string]string{"reason": "Indiquez le motif du désistement (2 000 caractères au plus)."})
	}
	if !validUUID(in.ApplicationID) || in.ListVersion < 1 {
		return View{}, admin.NewProblem(400, "INVALID_PARAMETERS", "Décision de sélection invalide.")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := s.offer(ctx, tx, actor, offer, admin.PermSelectionManage, false)
	if err != nil {
		return View{}, err
	}
	if !o.Frozen {
		return View{}, ErrNotFrozen
	}
	if _, err = tx.Exec(ctx, `SELECT 1 FROM ranking_freezes WHERE campaign_program_id=$1::uuid FOR UPDATE`, o.ID); err != nil {
		return View{}, err
	}
	v, err := s.frozen(ctx, tx, o, 0)
	if err != nil {
		return View{}, err
	}
	if v.Freeze.ListVersion != in.ListVersion {
		return View{}, ErrVersion
	}
	state := ""
	for _, e := range v.Entries {
		if e.ApplicationID == in.ApplicationID {
			state = e.Decision
		}
	}
	switch {
	case state == "":
		return View{}, ErrEntryNotFound
	case kind == "WITHDRAWN" && state != Admitted && state != Waitlisted:
		return View{}, ErrState
	case kind == "PROMOTED" && state != Waitlisted:
		return View{}, ErrState
	case kind == "PROMOTED" && v.Freeze.FreeSeats == 0:
		return View{}, ErrNoFreeSeat
	case kind == "PROMOTED" && (v.Proposal == nil || v.Proposal.ApplicationID != in.ApplicationID):
		return View{}, ErrOrder
	}
	var nullable *string
	if kind == "WITHDRAWN" {
		nullable = &reason
	}
	if _, err = tx.Exec(ctx, `INSERT INTO selection_events(campaign_program_id,list_version,event_type,application_id,reason,actor_id)
	 VALUES($1::uuid,$2,$3,$4::uuid,$5,$6::uuid)`, o.ID, in.ListVersion+1, kind, in.ApplicationID, nullable, actor.UserID); err != nil {
		return View{}, err
	}
	event := map[string]string{"WITHDRAWN": "SELECTION_WITHDRAWN", "PROMOTED": "SELECTION_PROMOTED"}[kind]
	if err = record(ctx, tx, actor.UserID, event, o.ID, map[string]any{"applicationId": in.ApplicationID, "listVersion": in.ListVersion + 1, "previousDecision": state}); err != nil {
		return View{}, err
	}
	after, err := s.frozen(ctx, tx, o, 0)
	if err != nil {
		return View{}, err
	}
	return after, tx.Commit(ctx)
}

// now is the clock of export headers (tests may fix it).
var now = time.Now
