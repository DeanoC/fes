package metadata

import (
	"sort"
	"strings"
)

type MatchDecision struct {
	Outcome   Outcome
	Candidate Candidate
	Score     int
}

// MatchCandidates performs deterministic, platform-scoped matching. The
// returned candidate is a detached copy and never aliases the provider result.
func MatchCandidates(input LookupInput, platformID string, candidates []Candidate) (MatchDecision, error) {
	primary, err := NormalizeTitle(input.Title)
	if err != nil {
		return MatchDecision{}, newOpError(ErrPolicyBlocked, err)
	}
	decorated, err := DecoratedTitle(input.Title)
	if err != nil {
		return MatchDecision{}, newOpError(ErrPolicyBlocked, err)
	}
	if strings.TrimSpace(platformID) == "" {
		return MatchDecision{Outcome: OutcomeNoMatch}, nil
	}

	deduplicated, conflict := deduplicateCandidates(candidates)
	if conflict {
		return MatchDecision{Outcome: OutcomeAmbiguous}, nil
	}
	type scored struct {
		candidate Candidate
		score     int
	}
	matches := make([]scored, 0, len(deduplicated))
	for _, candidate := range deduplicated {
		if !hasPlatform(candidate.PlatformIDs, platformID) || candidate.ProviderID == "" {
			continue
		}
		candidatePrimary, err := NormalizeTitle(candidate.Name)
		if err != nil {
			continue
		}
		candidateDecorated, err := DecoratedTitle(candidate.Name)
		if err != nil {
			continue
		}
		score := 0
		if primary == candidatePrimary {
			score = 100
		} else {
			for _, alternate := range candidate.AlternativeNames {
				alternatePrimary, normalizeErr := NormalizeTitle(alternate)
				if normalizeErr == nil && alternatePrimary == primary {
					score = 100
					break
				}
			}
		}
		if score == 0 && decorated == candidateDecorated && (decorated != primary || candidateDecorated != candidatePrimary) {
			score = 95
		} else if score == 0 {
			for _, alternate := range candidate.AlternativeNames {
				alternateDecorated, normalizeErr := DecoratedTitle(alternate)
				alternatePrimary, primaryErr := NormalizeTitle(alternate)
				if normalizeErr == nil && primaryErr == nil && decorated == alternateDecorated && (decorated != primary || alternateDecorated != alternatePrimary) {
					score = 95
					break
				}
			}
		}
		if score >= 95 {
			matches = append(matches, scored{candidate: cloneCandidate(candidate), score: score})
		}
	}
	if len(matches) == 0 {
		return MatchDecision{Outcome: OutcomeNoMatch}, nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].candidate.ProviderID < matches[j].candidate.ProviderID
	})
	best := matches[0].score
	bestCount := 0
	for _, match := range matches {
		if match.score != best {
			break
		}
		bestCount++
	}
	if bestCount != 1 {
		return MatchDecision{Outcome: OutcomeAmbiguous}, nil
	}
	outcome := OutcomeConfident
	if best == 100 {
		outcome = OutcomeExact
	}
	return MatchDecision{Outcome: outcome, Candidate: matches[0].candidate, Score: best}, nil
}

// Match is a concise alias for callers that already hold a resolved platform.
func Match(input LookupInput, platformID string, candidates []Candidate) (MatchDecision, error) {
	return MatchCandidates(input, platformID, candidates)
}

func deduplicateCandidates(candidates []Candidate) ([]Candidate, bool) {
	byID := make(map[string]Candidate, len(candidates))
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ProviderID == "" {
			continue
		}
		canonical := cloneCandidate(candidate)
		if previous, ok := byID[candidate.ProviderID]; ok {
			if !sameCandidateIdentity(previous, canonical) {
				return nil, true
			}
			continue
		}
		byID[candidate.ProviderID] = canonical
		ids = append(ids, candidate.ProviderID)
	}
	sort.Strings(ids)
	result := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		result = append(result, byID[id])
	}
	return result, false
}

func sameCandidateIdentity(left, right Candidate) bool {
	leftPlatforms := append([]string(nil), left.PlatformIDs...)
	rightPlatforms := append([]string(nil), right.PlatformIDs...)
	sort.Strings(leftPlatforms)
	sort.Strings(rightPlatforms)
	return left.Name == right.Name && strings.Join(leftPlatforms, "\x00") == strings.Join(rightPlatforms, "\x00")
}

func hasPlatform(platforms []string, wanted string) bool {
	for _, platform := range platforms {
		if platform == wanted {
			return true
		}
	}
	return false
}

func cloneCandidate(candidate Candidate) Candidate {
	candidate.AlternativeNames = append([]string(nil), candidate.AlternativeNames...)
	candidate.PlatformIDs = append([]string(nil), candidate.PlatformIDs...)
	candidate.Genres = append([]string(nil), candidate.Genres...)
	candidate.Studios = append([]string(nil), candidate.Studios...)
	candidate.Artwork = append([]ArtworkRef(nil), candidate.Artwork...)
	return candidate
}
