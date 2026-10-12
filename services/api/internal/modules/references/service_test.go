package references

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRepository struct {
	campaigns                        []Campaign
	rule                             Rule
	programError, qualificationError error
}

func (f fakeRepository) Campaigns(context.Context, string, time.Time) ([]Campaign, error) {
	return f.campaigns, nil
}
func (f fakeRepository) Program(_ context.Context, c string) (Program, error) {
	return Program{c, "Programme"}, f.programError
}
func (f fakeRepository) Qualification(_ context.Context, c string) (Qualification, error) {
	return Qualification{c, "Qualification", "BAC"}, f.qualificationError
}
func (f fakeRepository) Programs(context.Context, string, string) ([]OfferedProgram, error) {
	return []OfferedProgram{}, nil
}
func (f fakeRepository) Qualifications(context.Context) ([]Qualification, error) {
	return []Qualification{}, nil
}
func (f fakeRepository) EligibleQualifications(context.Context, string, string) ([]Qualification, error) {
	return []Qualification{}, nil
}
func (f fakeRepository) Rule(context.Context, string, string, string) (Rule, error) {
	return f.rule, nil
}
func fixtureRepository() fakeRepository {
	return fakeRepository{campaigns: []Campaign{{ID: "test", Code: "TEST", Status: "OPEN", StartsAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), EndsAt: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}}, rule: Rule{Known: true, Eligible: true, Version: 1, Subjects: []ExpectedSubjectRule{{"MATH", "Mathématiques", 1}, {"FRENCH", "Français", 2}, {"ENGLISH", "Anglais", 3}}}}
}
func TestResolutionSafety(t *testing.T) {
	cases := []struct {
		name     string
		change   func(*fakeRepository)
		err      error
		eligible bool
		reason   string
	}{
		{"eligible", func(*fakeRepository) {}, nil, true, "ELIGIBLE"},
		{"unknown combination", func(f *fakeRepository) { f.rule = Rule{} }, nil, false, "RULE_NOT_DEFINED"},
		{"explicit refusal", func(f *fakeRepository) { f.rule.Eligible = false; f.rule.Subjects = nil }, nil, false, "INELIGIBLE"},
		{"incomplete", func(f *fakeRepository) { f.rule.Subjects = nil }, ErrIncompleteRules, false, ""},
		{"duplicate", func(f *fakeRepository) { f.rule.Subjects[2].Code = "MATH" }, ErrIncompleteRules, false, ""},
		{"missing position", func(f *fakeRepository) { f.rule.Subjects[2].Position = 4 }, ErrIncompleteRules, false, ""},
		{"program unknown", func(f *fakeRepository) { f.programError = ErrProgramNotFound }, ErrProgramNotFound, false, ""},
		{"qualification unknown", func(f *fakeRepository) { f.qualificationError = ErrQualificationNotFound }, ErrQualificationNotFound, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureRepository()
			tc.change(&f)
			out, err := NewService(f).Resolve(context.Background(), "", "GL", "BAC_C")
			if !errors.Is(err, tc.err) {
				t.Fatalf("error %v", err)
			}
			if err == nil && (out.Eligible != tc.eligible || out.Reason != tc.reason) {
				t.Fatalf("resolution %+v", out)
			}
		})
	}
}
func TestCampaignWindowAndAmbiguity(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*fakeRepository)
		code   string
		err    error
	}{
		{"none", func(f *fakeRepository) { f.campaigns = nil }, "", ErrNoCurrentCampaign},
		{"unknown explicit", func(f *fakeRepository) { f.campaigns = nil }, "UNKNOWN", ErrCampaignNotFound},
		{"multiple", func(f *fakeRepository) { f.campaigns = append(f.campaigns, f.campaigns[0]) }, "", ErrAmbiguousCampaign},
		{"closed", func(f *fakeRepository) { f.campaigns[0].Status = "CLOSED" }, "TEST", ErrCampaignNotOpen},
		{"end excluded", func(f *fakeRepository) { f.campaigns[0].EndsAt = now }, "TEST", ErrCampaignNotOpen},
		{"start included", func(f *fakeRepository) { f.campaigns[0].StartsAt = now }, "TEST", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureRepository()
			tc.change(&f)
			s := NewService(f)
			s.now = func() time.Time { return now }
			_, err := s.Campaign(context.Background(), tc.code)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error %v", err)
			}
		})
	}
}
func TestExpectedRuleHasOnlySubjectFields(t *testing.T) {
	typ := reflect.TypeFor[ExpectedSubjectRule]()
	want := []string{"Code", "Name", "Position"}
	if typ.NumField() != len(want) {
		t.Fatal("unexpected subject fields")
	}
	for i, name := range want {
		if typ.Field(i).Name != name {
			t.Fatal("unexpected subject field")
		}
	}
	data, _ := json.Marshal(fixtureRepository().rule.Subjects)
	for _, field := range []string{"coefficient", "grade", "weight", "note"} {
		if strings.Contains(strings.ToLower(string(data)), field) {
			t.Fatalf("forbidden field %s", field)
		}
	}
}
func TestHTTPReferences(t *testing.T) {
	for _, tc := range []struct {
		path   string
		change func(*fakeRepository)
		status int
		code   string
	}{
		{"/api/v1/campaigns/current", nil, 200, ""},
		{"/api/v1/programs", nil, 200, ""},
		{"/api/v1/programs?qualification=BAC_C", nil, 200, ""},
		{"/api/v1/origin-qualifications", nil, 200, ""},
		{"/api/v1/programs/GL/eligible-qualifications", nil, 200, ""},
		{"/api/v1/eligibility?program=GL&qualification=BAC_C", nil, 200, ""},
		{"/api/v1/expected-subjects?program=GL&qualification=BAC_E", nil, 200, ""},
		{"/api/v1/eligibility?program=GL", nil, 400, "INVALID_PARAMETERS"},
		{"/api/v1/programs?qualification=bad", nil, 400, "INVALID_PARAMETERS"},
		{"/api/v1/programs?campaign=", nil, 400, "INVALID_PARAMETERS"},
		{"/api/v1/programs?campaign=A&campaign=B", nil, 400, "INVALID_PARAMETERS"},
		{"/api/v1/programs?campaign=%ZZ", nil, 400, "INVALID_PARAMETERS"},
		{"/api/v1/origin-qualifications?campaign=TEST", nil, 400, "INVALID_PARAMETERS"},
		{"/api/v1/eligibility?program=UNKNOWN&qualification=BAC_C", func(f *fakeRepository) { f.programError = ErrProgramNotFound }, 404, "PROGRAM_NOT_FOUND"},
		{"/api/v1/eligibility?program=GL&qualification=UNKNOWN", func(f *fakeRepository) { f.qualificationError = ErrQualificationNotFound }, 404, "QUALIFICATION_NOT_FOUND"},
		{"/api/v1/eligibility?program=IA&qualification=DT_DWM", func(f *fakeRepository) { f.rule = Rule{} }, 200, ""},
		{"/api/v1/expected-subjects?program=GL&qualification=BAC_C", func(f *fakeRepository) { f.rule.Eligible = false }, 200, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			f := fixtureRepository()
			if tc.change != nil {
				tc.change(&f)
			}
			mux := http.NewServeMux()
			Register(mux, NewService(f), slog.New(slog.NewTextHandler(io.Discard, nil)))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if tc.code != "" {
				var body map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["code"] != tc.code {
					t.Fatalf("body %s", w.Body)
				}
			}
		})
	}
}
