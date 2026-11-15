package domain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// predictableWords are first-label words that scanners and blocklists try
// first against a new host. The list is the one the maintainer decided on
// (TECH.md, slice 16).
var predictableWords = map[string]bool{
	"vpn": true, "node": true, "panel": true, "proxy": true,
	"admin": true, "xray": true, "vless": true, "remna": true,
}

// countryCodes holds the 249 ISO 3166-1 alpha-2 codes plus "uk" and "eu",
// which people use in host names although ISO does not assign them as
// country codes.
var countryCodes = func() map[string]bool {
	const iso = "AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ " +
		"BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ " +
		"CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ " +
		"DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR " +
		"GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY " +
		"HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP " +
		"KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY " +
		"MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ " +
		"NA NC NE NF NG NI NL NO NP NR NU NZ OM " +
		"PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW " +
		"SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ " +
		"TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ " +
		"UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW"
	m := map[string]bool{"uk": true, "eu": true}
	for _, c := range strings.Fields(iso) {
		m[strings.ToLower(c)] = true
	}
	return m
}()

// PolicyError reports the part of the first label that makes a name
// predictable.
type PolicyError struct {
	Part    string
	Country bool
}

func (e *PolicyError) Error() string {
	if e.Country {
		return fmt.Sprintf("first label contains the country code %q", e.Part)
	}
	return fmt.Sprintf("first label contains the predictable word %q", e.Part)
}

// ErrPolicy is matched with errors.Is for any PolicyError.
var ErrPolicy = errors.New("predictable domain name")

func (e *PolicyError) Is(target error) bool { return target == ErrPolicy }

// CheckPolicy rejects a name whose first label is, or contains as a
// separate part, a predictable word or a country code. Parts are the
// letter runs between hyphens and digits, so "nl1", "de-node" and "vpn01"
// are rejected while "stats" and "metrics" pass. Only the first label is
// examined: the base domain belongs to the operator. A name that is not
// syntactically valid is left to ValidateName.
func CheckPolicy(name string) error {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if ValidateName(name) != nil {
		return nil
	}
	label := strings.SplitN(name, ".", 2)[0]
	parts := strings.FieldsFunc(label, func(r rune) bool { return r < 'a' || r > 'z' })
	for _, p := range parts {
		if predictableWords[p] {
			return &PolicyError{Part: p}
		}
		if countryCodes[p] {
			return &PolicyError{Part: p, Country: true}
		}
	}
	return nil
}

// policyMessage renders err for the operator in the current language.
func policyMessage(name string, err *PolicyError) string {
	reason := i18n.T("DOMAIN_POLICY_WORD")
	if err.Country {
		reason = i18n.T("DOMAIN_POLICY_COUNTRY")
	}
	return fmt.Sprintf(i18n.T("DOMAIN_POLICY"), name, fmt.Sprintf(reason, err.Part))
}

// Read prompts with the i18n key promptKey until the operator enters a name
// that passes CheckPolicy. Every prompt for a new domain goes through Read,
// before any DNS or certificate work; names that are syntactically invalid
// are returned as typed and rejected later by CheckDomain.
func Read(promptKey string) string {
	return readWith(ui.Reading, promptKey)
}

func readWith(ask func(string) string, promptKey string) string {
	for {
		name := ask(i18n.T(promptKey))
		var pe *PolicyError
		if err := CheckPolicy(name); errors.As(err, &pe) {
			fmt.Printf("%s%s%s\n", ui.ColorRed, policyMessage(name, pe), ui.ColorReset)
			continue
		}
		return name
	}
}
