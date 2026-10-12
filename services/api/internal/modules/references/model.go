package references

import (
	"errors"
	"time"
)

var (
	ErrCampaignNotFound      = errors.New("CAMPAIGN_NOT_FOUND")
	ErrNoCurrentCampaign     = errors.New("NO_CURRENT_CAMPAIGN")
	ErrAmbiguousCampaign     = errors.New("AMBIGUOUS_CURRENT_CAMPAIGN")
	ErrCampaignNotOpen       = errors.New("CAMPAIGN_NOT_OPEN")
	ErrProgramNotFound       = errors.New("PROGRAM_NOT_FOUND")
	ErrQualificationNotFound = errors.New("QUALIFICATION_NOT_FOUND")
	ErrIncompleteRules       = errors.New("RULE_SET_INCOMPLETE")
)

type Campaign struct {
	ID           string    `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	AcademicYear string    `json:"academicYear"`
	StartsAt     time.Time `json:"startsAt"`
	EndsAt       time.Time `json:"endsAt"`
	Status       string    `json:"status"`
}
type Program struct {
	Code string `json:"code"`
	Name string `json:"name"`
}
type OfferedProgram struct {
	Program
	AvailableSeats *int `json:"availableSeats"`
}
type Qualification struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Family string `json:"family"`
}

// ExpectedSubjectRule deliberately contains neither grade nor coefficient.
type ExpectedSubjectRule struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}
type Rule struct {
	Known    bool
	Eligible bool
	Version  int
	Subjects []ExpectedSubjectRule
}
type Resolution struct {
	Campaign         Campaign              `json:"campaign"`
	Program          Program               `json:"program"`
	Qualification    Qualification         `json:"qualification"`
	Eligible         bool                  `json:"eligible"`
	Reason           string                `json:"reason"`
	RuleSetVersion   *int                  `json:"ruleSetVersion"`
	ExpectedSubjects []ExpectedSubjectRule `json:"expectedSubjects"`
}
