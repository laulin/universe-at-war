package ai

import (
	"context"

	appalliance "universeatwar/internal/app/alliance"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainai "universeatwar/internal/domain/ai"
	domainalliance "universeatwar/internal/domain/alliance"
)

// Diplomacy is the surface an artificial leader uses to answer what other
// alliances declare about its own. It goes through the ordinary use cases: a
// declaration is still a declaration, and a member without the right to make one
// still cannot make it.
type Diplomacy interface {
	Profile(context.Context, appauth.Principal) (appalliance.Profile, error)
	Declare(context.Context, appauth.Principal, string, domainalliance.Relation) error
}

// answerDeclarations is what the leader of an alliance does about the
// declarations other alliances have made towards it. It reads nothing but what
// was announced to it, and answers in kind, as often as the rule of the universe
// allows it to revise its mind.
func (b *Brain) answerDeclarations(ctx context.Context, principal appauth.Principal,
	home appeconomy.Planet) []domainai.Decision {
	mode := domainai.Diplomacy(home.Rules.AI.Diplomacy)
	if b.Diplomacy == nil || mode == domainai.NeverDeclares || !mode.Valid() {
		return nil
	}
	profile, err := b.Diplomacy.Profile(ctx, principal)
	if err != nil {
		return []domainai.Decision{failure(domainai.Strategic, "diplomacy", err)}
	}
	answered := make(map[string]domainalliance.Relation, len(profile.Relations))
	for _, relation := range profile.Relations {
		answered[relation.OtherTag] = relation.Kind
	}
	var decisions []domainai.Decision
	for _, heard := range profile.Received {
		standing, has := answered[heard.OtherTag]
		answer, say := domainai.Answer(mode, heard.Kind, standing, has)
		if !say {
			continue
		}
		action := "declare " + string(answer) + " on " + heard.OtherTag
		if err := b.Diplomacy.Declare(ctx, principal, heard.OtherTag, answer); err != nil {
			decisions = append(decisions, failure(domainai.Strategic, action, err))
			continue
		}
		decisions = append(decisions, domainai.Decision{
			Layer: domainai.Strategic, Action: action, Outcome: domainai.Done,
			Reason: "answering what " + heard.OtherTag + " declared about the alliance",
		})
	}
	return decisions
}
