package references

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func Register(mux *http.ServeMux, service *Service, logger *slog.Logger) {
	h := Handler{service, logger}
	for _, route := range []string{"/campaigns/current", "/programs", "/origin-qualifications", "/programs/{programCode}/eligible-qualifications", "/eligibility", "/expected-subjects"} {
		mux.HandleFunc("GET /api/v1"+route, h.serve)
	}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (h Handler) serve(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	params, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeJSON(w, 400, map[string]string{"code": "INVALID_PARAMETERS", "message": "Paramètres invalides."})
		return
	}
	allowed := map[string]bool{"campaign": true}
	switch r.URL.Path {
	case "/api/v1/programs":
		allowed["qualification"] = true
	case "/api/v1/eligibility", "/api/v1/expected-subjects":
		allowed["program"] = true
		allowed["qualification"] = true
	case "/api/v1/origin-qualifications":
		allowed = map[string]bool{}
	}
	// Reject malformed encoding, duplicate, unknown and empty parameters rather than silently ignoring them.
	for key, values := range params {
		if !allowed[key] || len(values) != 1 || !codePattern.MatchString(values[0]) {
			writeJSON(w, 400, map[string]string{"code": "INVALID_PARAMETERS", "message": "Paramètres invalides."})
			return
		}
	}
	campaign, program, qualification := params.Get("campaign"), params.Get("program"), params.Get("qualification")
	var result any
	switch r.URL.Path {
	case "/api/v1/campaigns/current":
		result, err = h.service.Campaign(ctx, campaign)
	case "/api/v1/programs":
		result, err = h.service.Programs(ctx, campaign, qualification)
	case "/api/v1/origin-qualifications":
		result, err = h.service.Qualifications(ctx)
	case "/api/v1/eligibility", "/api/v1/expected-subjects":
		if program == "" || qualification == "" {
			writeJSON(w, 400, map[string]string{"code": "INVALID_PARAMETERS", "message": "program et qualification sont obligatoires."})
			return
		}
		result, err = h.service.Resolve(ctx, campaign, program, qualification)
	default:
		program = r.PathValue("programCode")
		if !codePattern.MatchString(program) {
			writeJSON(w, 400, map[string]string{"code": "INVALID_PARAMETERS", "message": "Code filière invalide."})
			return
		}
		result, err = h.service.EligibleQualifications(ctx, campaign, program)
	}
	if err == nil {
		writeJSON(w, 200, result)
		return
	}
	status, code := 500, "INTERNAL_ERROR"
	for _, known := range []error{ErrCampaignNotFound, ErrNoCurrentCampaign, ErrProgramNotFound, ErrQualificationNotFound} {
		if errors.Is(err, known) {
			status, code = 404, known.Error()
		}
	}
	for _, known := range []error{ErrAmbiguousCampaign, ErrCampaignNotOpen, ErrIncompleteRules} {
		if errors.Is(err, known) {
			status, code = 409, known.Error()
		}
	}
	if status == 500 {
		h.logger.Error("reference read failed", "path", r.URL.Path)
	}
	writeJSON(w, status, map[string]string{"code": code, "message": "Le référentiel demandé ne peut pas être résolu."})
}
