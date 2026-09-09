package ai

import domainalliance "universeatwar/internal/domain/alliance"

// Diplomacy is how an artificial alliance answers what other alliances declare
// about it. Whatever the setting, it reaches for nothing it was not told: a
// relation announces, and answering an announcement asks for no knowledge a
// member could not legitimately have.
type Diplomacy string

const (
	// NeverDeclares keeps an alliance silent whatever is said to it.
	NeverDeclares Diplomacy = "none"
	// DeclaresOnce forms a first impression and keeps it: having answered an
	// alliance once, it does not revise, however that alliance turns.
	DeclaresOnce Diplomacy = "static"
	// AnswersAlways follows what is actually said, so a pact that becomes a war
	// is answered in kind.
	AnswersAlways Diplomacy = "dynamic"
)

// Diplomacies lists every rule this build knows, in a stable order.
func Diplomacies() []Diplomacy {
	return []Diplomacy{NeverDeclares, DeclaresOnce, AnswersAlways}
}

// Valid reports whether the rule is one this build knows.
func (d Diplomacy) Valid() bool {
	switch d {
	case NeverDeclares, DeclaresOnce, AnswersAlways:
		return true
	default:
		return false
	}
}

// Answer is what an alliance declares back to one that declared something about
// it, and whether it declares anything at all. The answer is always in kind: an
// alliance that is told of a war answers a war, and one told of a pact answers a
// pact. Nothing is said twice, so a settled relation costs no further
// declaration, and a rule this build cannot read says nothing at all.
func Answer(mode Diplomacy, heard, answered domainalliance.Relation, hasAnswered bool) (domainalliance.Relation, bool) {
	if !heard.Valid() {
		return "", false
	}
	switch mode {
	case DeclaresOnce:
		if hasAnswered {
			return "", false
		}
	case AnswersAlways:
		if hasAnswered && answered == heard {
			return "", false
		}
	default:
		return "", false
	}
	return heard, true
}
