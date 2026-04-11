package models

// PersonaProfile defines the agent behavior configuration for a query-time persona
type PersonaProfile struct {
	Directives    []string          // Hard rules injected into the Reflect context
	Disposition   DispositionTraits // Soft personality traits (1-5 scale)
	SystemContext string            // Prepended to the query context
	MaxTokens     int32             // Response length budget
}

// DispositionTraits controls the agent's reasoning style (1-5 scale)
type DispositionTraits struct {
	Skepticism int32 // 1=trusting, 5=highly skeptical
	Literalism int32 // 1=creative interpretation, 5=strictly literal
	Empathy    int32 // 1=detached, 5=highly empathetic
}

// PersonaProfiles maps persona names to their behavior configuration.
// Users select a persona at query time; their RBAC system role controls access.
var PersonaProfiles = map[string]PersonaProfile{
	"developer": {
		Directives: []string{
			"Provide technical details including implementation specifics",
			"Include raw data references and direct quotes from source documents",
			"Reference specific section numbers and requirement levels",
		},
		Disposition:   DispositionTraits{Skepticism: 2, Literalism: 4, Empathy: 2},
		SystemContext: "The user is a technical developer who needs precise, detailed information.",
		MaxTokens:     8192,
	},
	"administrator": {
		Directives: []string{
			"Provide comprehensive information suitable for administrative decisions",
			"Include both technical details and management summaries",
			"Highlight compliance requirements and deadlines",
		},
		Disposition:   DispositionTraits{Skepticism: 3, Literalism: 3, Empathy: 3},
		SystemContext: "The user is an administrator managing compliance and operations.",
		MaxTokens:     6144,
	},
	"manager": {
		Directives: []string{
			"Focus on summaries and actionable insights",
			"Present information in terms of business impact and risk",
			"Avoid excessive technical detail unless specifically requested",
		},
		Disposition:   DispositionTraits{Skepticism: 3, Literalism: 2, Empathy: 4},
		SystemContext: "The user is a manager who needs decision-focused summaries.",
		MaxTokens:     4096,
	},
	"auditor": {
		Directives: []string{
			"Always cite specific document references with section numbers",
			"Evaluate compliance status: compliant, partially compliant, non-compliant",
			"Provide evidence-based answers with traceable source citations",
			"Flag any gaps or ambiguities in the available evidence",
		},
		Disposition:   DispositionTraits{Skepticism: 5, Literalism: 5, Empathy: 1},
		SystemContext: "The user is an auditor requiring evidence-based, compliance-focused responses.",
		MaxTokens:     6144,
	},
	"guest": {
		Directives: []string{
			"Provide general overviews only",
			"Do not disclose specific implementation details or internal metrics",
			"Keep responses concise and at a high level",
		},
		Disposition:   DispositionTraits{Skepticism: 3, Literalism: 3, Empathy: 4},
		SystemContext: "The user has limited access. Provide general information only.",
		MaxTokens:     2048,
	},
}

// personaRBACLevel maps each persona to the minimum RBAC role required to use it.
// Users can select any persona whose required role is at or below their own system role.
var personaRBACLevel = map[string]string{
	"developer":     "developer",
	"administrator": "administrator",
	"auditor":       "administrator", // auditor persona requires admin or higher
	"manager":       "manager",
	"guest":         "guest",
}

// rbacHierarchy mirrors the RBAC hierarchy from shared/middleware/auth.go.
// Each role lists all roles it has permission to act as.
var rbacHierarchy = map[string][]string{
	"super_admin":   {"super_admin", "administrator", "developer", "manager", "operator", "guest"},
	"administrator": {"administrator", "developer", "manager", "operator", "guest"},
	"developer":     {"developer", "manager", "operator", "guest"},
	"manager":       {"manager", "operator", "guest"},
	"operator":      {"operator", "guest"},
	"guest":         {"guest"},
}

// CanUsePersona checks if a user with the given system role can use the specified persona
func CanUsePersona(userRole, persona string) bool {
	requiredRole, ok := personaRBACLevel[persona]
	if !ok {
		return false
	}
	permissions, exists := rbacHierarchy[userRole]
	if !exists {
		return false
	}
	for _, perm := range permissions {
		if perm == requiredRole {
			return true
		}
	}
	return false
}

// DefaultPersonaForRole returns the default persona for a given RBAC role
func DefaultPersonaForRole(role string) string {
	switch role {
	case "super_admin", "developer":
		return "developer"
	case "administrator":
		return "administrator"
	case "manager", "operator":
		return "manager"
	case "guest":
		return "guest"
	default:
		return "guest"
	}
}
