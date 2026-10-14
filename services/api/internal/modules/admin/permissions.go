package admin

import (
	"slices"
	"sort"
)

// Permission is what the code checks; roles are only bundles of permissions.
type Permission string

const (
	// Route markers: every admin route declares one of these or a business permission.
	PermPublic       Permission = "public"                   // anonymous (login, activation)
	PermAnySession   Permission = "session.any"              // any stage: session read, logout
	PermEnrollment   Permission = "session.mfa-enrollment"   // first login, TOTP enrolment
	PermVerification Permission = "session.mfa-verification" // TOTP or recovery code
	PermSelf         Permission = "self.manage"              // any active account: own password, codes

	PermDashboard      Permission = "dashboard.view"
	PermAccountsRead   Permission = "accounts.read"
	PermAccountsManage Permission = "accounts.manage"
	PermAuditRead      Permission = "audit.read"
	// Phase 8: review of submitted applications and administrative corrections.
	PermApplicationsRead   Permission = "applications.read"
	PermReviewClaim        Permission = "review.claim"    // take a dossier "to review"
	PermReviewDecide       Permission = "review.decide"   // validate or reject a dossier one holds
	PermReviewReassign     Permission = "review.reassign" // move a dossier to another agent
	PermReviewRevert       Permission = "review.revert"   // send a decided dossier back to review
	PermCorrectionsPropose Permission = "corrections.propose"
	PermCorrectionsDecide  Permission = "corrections.decide"
	// Declared for phase 9: the ranking is computed automatically, then validated.
	PermRankingRead       Permission = "ranking.read"
	PermSelectionValidate Permission = "selection.validate"
)

type Role string

// IFRI organisation (decisions of the phase 8 brief). One role per account.
const (
	RoleSuperAdmin     Role = "SUPER_ADMIN"     // technical administrator: accounts and audit, no dossier
	RoleReviewer       Role = "REVIEWER"        // agent of the executive secretariat
	RoleReviewLead     Role = "REVIEW_LEAD"     // head of the executive secretariat
	RoleDeputyDirector Role = "DEPUTY_DIRECTOR" // sole approver of corrections
	RoleDirector       Role = "DIRECTOR"        // reads everything, validates the final selection
)

var reviewer = []Permission{PermDashboard, PermApplicationsRead, PermReviewClaim, PermReviewDecide, PermCorrectionsPropose, PermRankingRead}

// RolePermissions is the whole RBAC policy.
var RolePermissions = map[Role][]Permission{
	RoleSuperAdmin:     {PermDashboard, PermAccountsRead, PermAccountsManage, PermAuditRead},
	RoleReviewer:       reviewer,
	RoleReviewLead:     append(append([]Permission{}, reviewer...), PermReviewReassign, PermReviewRevert),
	RoleDeputyDirector: {PermDashboard, PermApplicationsRead, PermCorrectionsDecide, PermRankingRead},
	// The audit holds the technical administrator's own actions: the Director reads it too.
	RoleDirector: {PermDashboard, PermApplicationsRead, PermAuditRead, PermRankingRead, PermSelectionValidate},
}

// GlobalRoles can never be limited to programs. The phase 7 scope mechanism stays available
// for the agents' roles but the organisation does not use it.
var GlobalRoles = map[Role]bool{RoleSuperAdmin: true, RoleDeputyDirector: true, RoleDirector: true}

// ProgramScoped permissions apply per program; the others are global by nature.
var ProgramScoped = map[Permission]bool{
	PermApplicationsRead: true, PermReviewClaim: true, PermReviewDecide: true, PermReviewReassign: true, PermReviewRevert: true,
	PermCorrectionsPropose: true, PermCorrectionsDecide: true, PermRankingRead: true, PermSelectionValidate: true,
}

func KnownRole(r Role) bool { _, ok := RolePermissions[r]; return ok }

// Grant is one active role assignment; ProgramID empty means every program.
type Grant struct {
	Role        Role   `json:"role"`
	ProgramID   string `json:"-"`
	ProgramCode string `json:"program,omitempty"`
	ProgramName string `json:"programName,omitempty"`
}

// Principal is the authenticated administrator of an active session.
type Principal struct {
	UserID      string
	Email       string
	DisplayName string
	Grants      []Grant
}

func grants(role Role, p Permission) bool { return slices.Contains(RolePermissions[role], p) }

// Can reports a permission held for at least one program (or globally).
func (p Principal) Can(perm Permission) bool {
	for _, g := range p.Grants {
		if grants(g.Role, perm) {
			return true
		}
	}
	return false
}

// CanFor reports a permission for one program: a global grant, or a grant on that program.
func (p Principal) CanFor(perm Permission, programID string) bool {
	for _, g := range p.Grants {
		if grants(g.Role, perm) && (g.ProgramID == "" || !ProgramScoped[perm] || g.ProgramID == programID) {
			return true
		}
	}
	return false
}

// Scope returns the programs a permission covers: all, or an explicit list (phase 8 filters).
func (p Principal) Scope(perm Permission) (all bool, programIDs []string) {
	for _, g := range p.Grants {
		if !grants(g.Role, perm) {
			continue
		}
		if g.ProgramID == "" || !ProgramScoped[perm] {
			return true, nil
		}
		if !slices.Contains(programIDs, g.ProgramID) {
			programIDs = append(programIDs, g.ProgramID)
		}
	}
	sort.Strings(programIDs)
	return false, programIDs
}

// Permissions lists the business permissions held, for the interface (never for decisions).
func (p Principal) Permissions() []Permission {
	out := []Permission{}
	for _, g := range p.Grants {
		for _, perm := range RolePermissions[g.Role] {
			if !slices.Contains(out, perm) {
				out = append(out, perm)
			}
		}
	}
	slices.Sort(out)
	return out
}
