package references

import (
	"context"
	"time"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service { return &Service{repository, time.Now} }
func (s *Service) Campaign(ctx context.Context, code string) (Campaign, error) {
	now := s.now().UTC()
	cs, err := s.repository.Campaigns(ctx, code, now)
	if err != nil {
		return Campaign{}, err
	}
	if len(cs) == 0 {
		if code == "" {
			return Campaign{}, ErrNoCurrentCampaign
		}
		return Campaign{}, ErrCampaignNotFound
	}
	if len(cs) > 1 {
		return Campaign{}, ErrAmbiguousCampaign
	}
	c := cs[0]
	if c.Status != "OPEN" || now.Before(c.StartsAt) || !now.Before(c.EndsAt) {
		return Campaign{}, ErrCampaignNotOpen
	}
	return c, nil
}
func (s *Service) Programs(ctx context.Context, campaign, qualification string) ([]OfferedProgram, error) {
	c, err := s.Campaign(ctx, campaign)
	if err != nil {
		return nil, err
	}
	if qualification != "" {
		if _, err = s.repository.Qualification(ctx, qualification); err != nil {
			return nil, err
		}
	}
	return s.repository.Programs(ctx, c.ID, qualification)
}
func (s *Service) Qualifications(ctx context.Context) ([]Qualification, error) {
	return s.repository.Qualifications(ctx)
}
func (s *Service) EligibleQualifications(ctx context.Context, campaign, program string) ([]Qualification, error) {
	c, err := s.Campaign(ctx, campaign)
	if err != nil {
		return nil, err
	}
	if _, err = s.repository.Program(ctx, program); err != nil {
		return nil, err
	}
	return s.repository.EligibleQualifications(ctx, c.ID, program)
}
func (s *Service) Resolve(ctx context.Context, campaign, program, qualification string) (Resolution, error) {
	c, err := s.Campaign(ctx, campaign)
	if err != nil {
		return Resolution{}, err
	}
	p, err := s.repository.Program(ctx, program)
	if err != nil {
		return Resolution{}, err
	}
	q, err := s.repository.Qualification(ctx, qualification)
	if err != nil {
		return Resolution{}, err
	}
	rule, err := s.repository.Rule(ctx, c.ID, program, qualification)
	if err != nil {
		return Resolution{}, err
	}
	out := Resolution{Campaign: c, Program: p, Qualification: q, Reason: "RULE_NOT_DEFINED", ExpectedSubjects: []ExpectedSubjectRule{}}
	if !rule.Known {
		return out, nil
	}
	out.RuleSetVersion = &rule.Version
	out.Reason = "INELIGIBLE"
	if !rule.Eligible {
		return out, nil
	}
	if len(rule.Subjects) == 0 {
		return Resolution{}, ErrIncompleteRules
	}
	seen := map[string]bool{}
	for i, subject := range rule.Subjects {
		if subject.Position != i+1 || seen[subject.Code] {
			return Resolution{}, ErrIncompleteRules
		}
		seen[subject.Code] = true
	}
	out.Eligible = true
	out.Reason = "ELIGIBLE"
	out.ExpectedSubjects = rule.Subjects
	return out, nil
}
