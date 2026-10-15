package selection

import (
	"bytes"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

func rat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	return r
}

// cand builds a candidate: weighted/coefficients as exact decimals, math "" for a grid without
// a mathematics subject, submission hours after t0.
func cand(number, weighted, coefficients, math string, hours int) Candidate {
	c := Candidate{ApplicationID: "id-" + number, Number: number, SubmittedAt: t0.Add(time.Duration(hours) * time.Hour),
		Weighted: rat(weighted), Coefficients: rat(coefficients)}
	if math != "" {
		c.MathSubject, c.MathGrade, c.MathSubjects = "MATH", rat(math), 1
	}
	return c
}

func order(list []Ranked) []string {
	out := []string{}
	for _, r := range list {
		out = append(out, r.Number)
	}
	return out
}

func seats(n int) *int { return &n }

func TestAverageIsComparedExactlyByCrossProduct(t *testing.T) {
	// 40/3 = 13.333… exceeds its own 30-decimal truncation: the decimal column would tie them.
	list := Rank([]Candidate{
		cand("B", "13.333333333333333333333333333333", "1", "10", 0),
		cand("A", "40", "3", "10", 5),
	}, nil, nil)
	if !slices.Equal(order(list), []string{"A", "B"}) || list[1].Decisive != CriterionAverage || list[0].Decisive != "" {
		t.Fatalf("%v %s", order(list), list[1].Decisive)
	}
	// 40/3 and 80/6 are the same fraction: an exact tie, decided by mathematics.
	list = Rank([]Candidate{cand("A", "40", "3", "12", 0), cand("B", "80", "6", "15", 5)}, nil, nil)
	if !slices.Equal(order(list), []string{"B", "A"}) || list[1].Decisive != CriterionMath || list[0].MathTiebreak != MathCompared {
		t.Fatalf("%v %+v", order(list), list[1])
	}
}

func TestMathematicsThenDateThenNumber(t *testing.T) {
	list := Rank([]Candidate{
		cand("D", "60", "5", "14", 3), // same math as C, later
		cand("C", "60", "5", "14", 1),
		cand("E", "60", "5", "14", 1), // same math and date as C: number
		cand("A", "65", "5", "8", 9),  // best average: mathematics never considered
		cand("B", "60", "5", "16", 9), // best mathematics of the tie group
	}, nil, nil)
	if !slices.Equal(order(list), []string{"A", "B", "C", "E", "D"}) {
		t.Fatal(order(list))
	}
	want := []string{"", CriterionAverage, CriterionMath, CriterionNumber, CriterionDate}
	for i, r := range list {
		if r.Decisive != want[i] || r.Rank != i+1 || r.Position != i+1 {
			t.Fatalf("%d %+v", i, r)
		}
	}
	if list[0].MathTiebreak != MathNotNeeded || list[1].MathTiebreak != MathCompared {
		t.Fatal("math tiebreak", list[0].MathTiebreak, list[1].MathTiebreak)
	}
}

// The example of docs/RANKING_SELECTION.md: a pairwise skip would give C > A (mathematics),
// A > B (date) and B > C (date), a cycle. The group rule skips mathematics for the whole group.
func TestMathematicsSkippedForTheWholeGroupWithoutMathSubject(t *testing.T) {
	list := Rank([]Candidate{
		cand("C", "60", "5", "15", 3),
		cand("B", "60", "5", "", 2), // DT/EAP: no mathematics subject in its grid
		cand("A", "60", "5", "10", 1),
	}, nil, nil)
	if !slices.Equal(order(list), []string{"A", "B", "C"}) {
		t.Fatal(order(list))
	}
	for _, r := range list {
		if r.MathTiebreak != MathSkipped {
			t.Fatal("mathematics compared in a group with a candidate without it", r.Number)
		}
	}
	if list[1].Decisive != CriterionDate || list[2].Decisive != CriterionDate {
		t.Fatal(list[1].Decisive, list[2].Decisive)
	}
	// Outside the group, a candidate without mathematics is ranked by average like anyone else.
	list = Rank([]Candidate{cand("X", "70", "5", "", 1), cand("Y", "60", "5", "20", 0)}, nil, nil)
	if !slices.Equal(order(list), []string{"X", "Y"}) || list[0].MathTiebreak != MathNotNeeded {
		t.Fatal(order(list))
	}
}

func TestSeatsAndMinimumAverage(t *testing.T) {
	pool := []Candidate{
		cand("A", "80", "5", "10", 0), // 16
		cand("B", "60", "5", "12", 0), // 12, tied with C at the seat line
		cand("C", "60", "5", "11", 0),
		cand("D", "50", "5", "10", 0), // exactly 10: "at least equal" to the minimum
		cand("E", "49.99", "5", "10", 0),
	}
	list := Rank(pool, seats(2), rat("10"))
	decisions := []string{}
	for _, r := range list {
		decisions = append(decisions, r.Number+":"+r.Decision)
	}
	if !slices.Equal(decisions, []string{"A:ADMITTED", "B:ADMITTED", "C:WAITLISTED", "D:WAITLISTED", "E:NOT_RANKED"}) {
		t.Fatal(decisions)
	}
	if list[4].Rank != 0 || list[4].Position != 5 || list[2].Decisive != CriterionMath {
		t.Fatalf("%+v", list[4])
	}
	// Seats left over never admit a candidate below the minimum.
	list = Rank(pool, seats(10000), rat("12.01"))
	for _, r := range list {
		if (r.Number == "A") != (r.Decision == Admitted) || (r.Number != "A" && r.Decision != NotRanked) {
			t.Fatal(r.Number, r.Decision)
		}
	}
	// One seat; as many seats as candidates; seats unknown (provisional): no decision.
	if list = Rank(pool, seats(1), nil); list[0].Decision != Admitted || list[1].Decision != Waitlisted {
		t.Fatal("one seat")
	}
	if list = Rank(pool, seats(5), nil); list[4].Decision != Admitted {
		t.Fatal("all admitted")
	}
	if list = Rank(pool, nil, nil); list[0].Decision != "" || list[4].Rank != 5 {
		t.Fatal("no seats yet")
	}
}

func TestWithdrawalsAndPromotionsFollowTheList(t *testing.T) {
	frozen := Rank([]Candidate{
		cand("A", "90", "5", "10", 0), cand("B", "85", "5", "10", 0), cand("C", "80", "5", "10", 0),
		cand("D", "75", "5", "10", 0), cand("E", "70", "5", "10", 0),
	}, seats(2), nil)
	states := States(frozen, nil, 1)
	if FreeSeats(2, states) != 0 {
		t.Fatal("no free seat at the freeze")
	}
	if first, ok := FirstWaiting(frozen, states); !ok || first.Number != "C" {
		t.Fatal("first waiting", first.Number)
	}
	events := []Event{
		{Version: 2, Type: "WITHDRAWN", ApplicationID: "id-A"},
		{Version: 3, Type: "WITHDRAWN", ApplicationID: "id-B"}, // two withdrawals in a row
		{Version: 4, Type: "PROMOTED", ApplicationID: "id-C"},
		{Version: 5, Type: "WITHDRAWN", ApplicationID: "id-D"}, // a waiting candidate withdraws
	}
	states = States(frozen, events, 5)
	if states["id-A"] != Withdrawn || states["id-C"] != Admitted || states["id-D"] != Withdrawn || FreeSeats(2, states) != 1 {
		t.Fatal(states)
	}
	if first, ok := FirstWaiting(frozen, states); !ok || first.Number != "E" {
		t.Fatal("next promotion skips nobody but the withdrawn", first.Number)
	}
	// A past version is rebuilt from the events up to it.
	if past := States(frozen, events, 3); past["id-C"] != Waitlisted || FreeSeats(2, past) != 2 {
		t.Fatal(past)
	}
}

func frozenView() View {
	rank := func(n int) *int { return &n }
	return View{
		Offer:  Offer{ProgramCode: "GL", ProgramName: "Génie Logiciel", CampaignCode: "DEV_LOCAL", CampaignName: "Démonstration"},
		Freeze: &Freeze{FrozenAt: t0, VersionAt: t0.Add(time.Hour), ListVersion: 2, Seats: 1},
		Entries: []Entry{
			{Position: 1, Rank: rank(1), Number: "IFRI-2026-GL-000001", LastName: "-Tiret", FirstNames: "Synthétique", Decision: Withdrawn, InitialDecision: Admitted,
				Average: Average{Display: "15.40"}, MathGrade: "16", MathTiebreak: MathNotNeeded},
			{Position: 2, Rank: rank(2), Number: "IFRI-2026-GL-000002", LastName: "=SOMME(A1)", FirstNames: "Élodie", Decision: Admitted, InitialDecision: Waitlisted,
				Average: Average{Display: "13.25"}, MathTiebreak: MathSkipped, DecisiveCriterion: CriterionAverage},
			{Position: 3, Rank: rank(3), Number: "IFRI-2026-GL-000003", LastName: "Houngbédji", FirstNames: "Ifèdé", Decision: Waitlisted, InitialDecision: Waitlisted,
				Average: Average{Display: "13.25"}, MathTiebreak: MathSkipped, DecisiveCriterion: CriterionDate},
			{Position: 4, Number: "IFRI-2026-GL-000004", LastName: "Sossa", FirstNames: "Léa", Decision: NotRanked, InitialDecision: NotRanked,
				Average: Average{Display: "08.10"}, MathTiebreak: MathNotNeeded, DecisiveCriterion: CriterionAverage},
		},
	}
}

func TestPublicListHoldsNoAverageNorWithdrawnNorNotRanked(t *testing.T) {
	d := document(frozenView(), VariantPublic, t0.Add(2*time.Hour))
	if !slices.Equal(d.Columns, []string{"Rang", "Numéro", "Nom", "Prénoms", "Décision"}) || len(d.Rows) != 2 {
		t.Fatal(d.Columns, d.Rows)
	}
	if !slices.Equal(d.Rows[0], []string{"2", "IFRI-2026-GL-000002", "=SOMME(A1)", "Élodie", "Admis"}) || d.Rows[1][4] != "Liste d’attente" {
		t.Fatal(d.Rows)
	}
	all := strings.Join(d.Columns, " ") + d.Title + strings.Join(d.Notes, " ")
	for _, r := range d.Rows {
		all += strings.Join(r, " ")
	}
	for _, h := range d.Header {
		all += h[0] + h[1]
	}
	for _, forbidden := range []string{"13,25", "15,40", "13.25", "oyenne", "Mathématiques", "@", "Désisté", "Non classé", "Sossa", "-Tiret"} {
		if strings.Contains(all, forbidden) {
			t.Fatal("public list contains", forbidden)
		}
	}
	if !strings.Contains(all, "Version 2 du 01/09/2026 10:00 (gel du 01/09/2026 09:00)") || !strings.Contains(all, "Généré le01/09/2026 11:00") {
		t.Fatal("version and dates in Benin time", d.Header)
	}
}

func TestInternalListShowsCriteriaAndSkippedMathematics(t *testing.T) {
	d := document(frozenView(), VariantInternal, t0)
	if len(d.Rows) != 4 || !d.Landscape {
		t.Fatal(len(d.Rows))
	}
	if d.Rows[2][6] != "13,25" || d.Rows[2][8] != "Maths non comparées : candidat sans matière mathématique dans le groupe" ||
		d.Rows[2][10] != "Date de soumission" || d.Rows[0][10] != "Premier de la liste" || d.Rows[3][1] != "" || d.Rows[3][12] != "Non classé" {
		t.Fatal(d.Rows)
	}
}

func TestCSVPrefixesFormulaCellsWithoutChangingThem(t *testing.T) {
	body := document(frozenView(), VariantInternal, t0).CSV()
	if !bytes.HasPrefix(body, []byte("\xef\xbb\xbf")) || !bytes.Contains(body, []byte("\r\n")) {
		t.Fatal("BOM and CRLF")
	}
	text := string(body)
	for _, want := range []string{";'-Tiret;Synthétique;", ";'=SOMME(A1);Élodie;", ";Houngbédji;Ifèdé;"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing", want, text)
		}
	}
	if strings.Contains(text, ";-Tiret;") || strings.Contains(text, ";=SOMME") {
		t.Fatal("formula cell left active")
	}
	for in, want := range map[string]string{"-12": "'-12", "+1": "'+1", "@x": "'@x", "\tA": "'\tA", "A-B": "A-B", "": "", "Élodie": "Élodie"} {
		if spreadsheetCell(in) != want {
			t.Fatal(in, spreadsheetCell(in))
		}
	}
}
