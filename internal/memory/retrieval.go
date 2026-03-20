package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// RetrievalContext holds all memory items retrieved for an agent session.
type RetrievalContext struct {
	Facts    []Fact
	Beliefs  []Belief
	Episodes []Episode
	Entities []Entity
}

// Retriever orchestrates hybrid search and graph traversal to assemble
// memory context for an agent session.
type Retriever struct {
	graphStore     *GraphStore
	factStore      *FactStore
	episodeStore   *EpisodeStore
	hybridSearcher *HybridSearcher
	ftsStore       *FTSStore
	maxGraphDepth  int
	topK           int
}

// NewRetriever creates a Retriever with the given stores and search
// configuration.
func NewRetriever(
	graphStore *GraphStore,
	factStore *FactStore,
	episodeStore *EpisodeStore,
	hybridSearcher *HybridSearcher,
	ftsStore *FTSStore,
	maxGraphDepth int,
	topK int,
) *Retriever {
	return &Retriever{
		graphStore:     graphStore,
		factStore:      factStore,
		episodeStore:   episodeStore,
		hybridSearcher: hybridSearcher,
		ftsStore:       ftsStore,
		maxGraphDepth:  maxGraphDepth,
		topK:           topK,
	}
}

// Retrieve assembles memory context for a session by running hybrid search,
// extracting entity mentions, traversing the graph, and loading related facts.
func (ret *Retriever) Retrieve(ctx context.Context, ticketDescription string, filePaths []string) (RetrievalContext, error) {
	searchFacts, searchBeliefs, searchEpisodes, err := ret.retrieveBySearch(ctx, ticketDescription)
	if err != nil {
		return RetrievalContext{}, fmt.Errorf("search retrieval: %w", err)
	}

	mentions := extractMentions(ticketDescription, filePaths)
	graphEntities, graphFacts := ret.retrieveByGraph(ctx, mentions)

	allFacts := make([]Fact, 0, len(searchFacts)+len(graphFacts))
	allFacts = append(allFacts, searchFacts...)
	allFacts = append(allFacts, graphFacts...)

	return rankAndDedup(allFacts, searchBeliefs, searchEpisodes, graphEntities, ret.topK), nil
}

func (ret *Retriever) retrieveBySearch(ctx context.Context, query string) ([]Fact, []Belief, []Episode, error) {
	results, err := ret.hybridSearcher.Search(ctx, query, ret.topK)
	if err != nil {
		return nil, nil, nil, err
	}

	var facts []Fact
	var beliefs []Belief
	var episodes []Episode

	for _, result := range results {
		switch result.Table {
		case "facts":
			fact, factErr := ret.factStore.GetFact(ctx, result.ID)
			if factErr != nil {
				continue
			}
			facts = append(facts, fact)
		case "beliefs":
			belief, beliefErr := ret.factStore.GetBelief(ctx, result.ID)
			if beliefErr != nil {
				continue
			}
			beliefs = append(beliefs, belief)
		case "episodes":
			episode, episodeErr := ret.episodeStore.GetEpisode(ctx, result.ID)
			if episodeErr != nil {
				continue
			}
			episodes = append(episodes, episode)
		}
	}

	return facts, beliefs, episodes, nil
}

func (ret *Retriever) retrieveByGraph(ctx context.Context, mentions []string) ([]Entity, []Fact) {
	allEntities := make([]Entity, 0, len(mentions))
	var allFacts []Fact

	for _, mention := range mentions {
		entity, found := ret.findEntityByMention(ctx, mention)
		if !found {
			continue
		}

		allEntities = append(allEntities, entity)
		ret.collectGraphContext(ctx, entity.ID, &allEntities, &allFacts)
	}

	return allEntities, allFacts
}

func (ret *Retriever) findEntityByMention(ctx context.Context, mention string) (Entity, bool) {
	entityTypes := []EntityType{EntityModule, EntityFile, EntityConcept, EntityPattern, EntityTool}
	for _, entityType := range entityTypes {
		entity, err := ret.graphStore.FindEntityByName(ctx, entityType, mention)
		if err == nil {
			return entity, true
		}
	}
	return Entity{}, false
}

func (ret *Retriever) collectGraphContext(ctx context.Context, entityID int64, entities *[]Entity, facts *[]Fact) {
	related, err := ret.graphStore.RelatedEntities(ctx, entityID, ret.maxGraphDepth)
	if err != nil {
		return
	}
	*entities = append(*entities, related...)

	entityFacts, err := ret.factStore.FactsByEntity(ctx, entityID)
	if err == nil {
		*facts = append(*facts, entityFacts...)
	}

	for _, relatedEntity := range related {
		relFacts, err := ret.factStore.FactsByEntity(ctx, relatedEntity.ID)
		if err != nil {
			continue
		}
		*facts = append(*facts, relFacts...)
	}
}

func extractMentions(ticketDescription string, filePaths []string) []string {
	combined := ticketDescription
	for _, filePath := range filePaths {
		combined += " " + filePath
	}

	for _, delimiter := range []string{"/", ".", "-", "_"} {
		combined = strings.ReplaceAll(combined, delimiter, " ")
	}

	seen := make(map[string]bool)
	tokens := strings.Fields(combined)
	mentions := make([]string, 0, len(tokens))

	for _, token := range tokens {
		lower := strings.ToLower(token)
		if len(lower) <= 2 {
			continue
		}
		if seen[lower] {
			continue
		}
		seen[lower] = true
		mentions = append(mentions, lower)
	}

	return mentions
}

func rankAndDedup(facts []Fact, beliefs []Belief, episodes []Episode, entities []Entity, topK int) RetrievalContext {
	dedupedFacts := dedupFacts(facts)
	dedupedBeliefs := dedupBeliefs(beliefs)
	dedupedEpisodes := dedupEpisodes(episodes)
	dedupedEntities := dedupEntities(entities)

	sort.Slice(dedupedFacts, func(i, j int) bool {
		return dedupedFacts[i].Confidence > dedupedFacts[j].Confidence
	})
	sort.Slice(dedupedBeliefs, func(i, j int) bool {
		return dedupedBeliefs[i].Confidence > dedupedBeliefs[j].Confidence
	})

	if len(dedupedFacts) > topK {
		dedupedFacts = dedupedFacts[:topK]
	}
	if len(dedupedBeliefs) > topK {
		dedupedBeliefs = dedupedBeliefs[:topK]
	}
	if len(dedupedEpisodes) > topK {
		dedupedEpisodes = dedupedEpisodes[:topK]
	}

	return RetrievalContext{
		Facts:    dedupedFacts,
		Beliefs:  dedupedBeliefs,
		Episodes: dedupedEpisodes,
		Entities: dedupedEntities,
	}
}

func dedupFacts(facts []Fact) []Fact {
	seen := make(map[int64]bool, len(facts))
	result := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		if seen[fact.ID] {
			continue
		}
		seen[fact.ID] = true
		result = append(result, fact)
	}
	return result
}

func dedupBeliefs(beliefs []Belief) []Belief {
	seen := make(map[int64]bool, len(beliefs))
	result := make([]Belief, 0, len(beliefs))
	for _, belief := range beliefs {
		if seen[belief.ID] {
			continue
		}
		seen[belief.ID] = true
		result = append(result, belief)
	}
	return result
}

func dedupEpisodes(episodes []Episode) []Episode {
	seen := make(map[int64]bool, len(episodes))
	result := make([]Episode, 0, len(episodes))
	for _, episode := range episodes {
		if seen[episode.ID] {
			continue
		}
		seen[episode.ID] = true
		result = append(result, episode)
	}
	return result
}

func dedupEntities(entities []Entity) []Entity {
	seen := make(map[int64]bool, len(entities))
	result := make([]Entity, 0, len(entities))
	for _, entity := range entities {
		if seen[entity.ID] {
			continue
		}
		seen[entity.ID] = true
		result = append(result, entity)
	}
	return result
}
