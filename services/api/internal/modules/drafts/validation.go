package drafts

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

func NormalizeProfile(input Profile) (Profile, error) {
	p := Profile{strings.TrimSpace(input.LastName), strings.TrimSpace(input.FirstNames), strings.ToLower(strings.TrimSpace(input.Email)), strings.TrimSpace(input.Phone)}
	fields := map[string]string{}
	for _, field := range []struct {
		key, value string
		limit      int
	}{{"lastName", p.LastName, 100}, {"firstNames", p.FirstNames, 150}} {
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit || strings.ContainsFunc(field.value, unicode.IsControl) {
			fields[field.key] = "Ce nom est trop long ou contient un caractère non autorisé."
		}
	}
	if p.Email != "" {
		address, err := mail.ParseAddress(p.Email)
		if err != nil || address.Address != p.Email || strings.ContainsAny(p.Email, "\r\n\t ") || !strings.Contains(strings.SplitN(p.Email, "@", 2)[len(strings.SplitN(p.Email, "@", 2))-1], ".") || len(p.Email) > 254 {
			fields["email"] = "Saisissez une adresse email valide."
		}
	}
	p.Phone = strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '(' || r == ')' {
			return -1
		}
		return r
	}, p.Phone)
	if strings.HasPrefix(p.Phone, "00") {
		p.Phone = "+" + p.Phone[2:]
	}
	if p.Phone != "" && !phonePattern.MatchString(p.Phone) {
		fields["phone"] = "Utilisez un numéro international, par exemple +229 01 23 45 67 89."
	}
	if len(fields) > 0 {
		return Profile{}, &Problem{Code: "VALIDATION_FAILED", Message: "Vérifiez les informations saisies.", Fields: fields, Status: 422}
	}
	return p, nil
}
