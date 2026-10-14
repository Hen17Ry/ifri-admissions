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
	// Declared for phase 8 (review) and phase 9 (ranking, selection); no route uses them yet.
	PermApplicationsRead   Permission = "applications.read"
	PermApplicationsReview Permission = "applications.review"
	PermRankingRead        Permission = "ranking.read"
	PermRankingManage      Permission = "ranking.manage"
	PermSelectionValidate  Permission = "selection.validate"
)

type Role string

const (
	RoleSuperAdmin       Role = "SUPER_ADMIN"
	RoleReviewer         Role = "REVIEWER"
	RoleSelectionManager Role = "SELECTION_MANAGER"
	RoleAuditor          Role = "AUDITOR"
)

// RolePermissions is the whole RBAC policy.
var RolePermissions = map[Role][]Permission{
	RoleSuperAdmin:       {PermDashboard, PermAccountsRead, PermAccountsManage, PermAuditRead},
	RoleReviewer:         {PermDashboard, PermApplicationsRead, PermApplicationsReview},
	RoleSelectionManager: {PermDashboard, PermApplicationsRead, PermRankingRead, PermRankingManage, PermSelectionValidate},
	RoleAuditor:          {PermDashboard, PermAccountsRead, PermAuditRead, PermApplicationsRead, PermRankingRead},
}

// GlobalRoles can never be limited to programs.
var GlobalRoles = map[Role]bool{RoleSuperAdmin: true, RoleAuditor: true}

// ProgramScoped permissions apply per program; the others are global by nature.
var ProgramScoped = map[Permission]bool{
	PermApplicationsRead: true, PermApplicationsReview: true,
	PermRankingRead: true, PermRankingManage: true, PermSelectionValidate: true,
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
