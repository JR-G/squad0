package slack

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

// Persona holds the display identity for an agent in Slack.
type Persona struct {
	Role    agent.Role
	Name    string
	IconURL string
}

// PersonaStore loads and saves agent personas backed by the memory DB.
type PersonaStore struct {
	graphStores map[agent.Role]*memory.GraphStore
	factStores  map[agent.Role]*memory.FactStore
}

// NewPersonaStore creates a PersonaStore with graph and fact stores for
// each agent's personal database.
func NewPersonaStore(graphStores map[agent.Role]*memory.GraphStore, factStores map[agent.Role]*memory.FactStore) *PersonaStore {
	return &PersonaStore{
		graphStores: graphStores,
		factStores:  factStores,
	}
}

// LoadPersona loads the persona for the given role from their memory DB.
// Returns a fallback persona using the role name if no chosen name exists.
func (store *PersonaStore) LoadPersona(ctx context.Context, role agent.Role) Persona {
	graphStore, ok := store.graphStores[role]
	if !ok {
		return fallbackPersona(role)
	}

	factStore, ok := store.factStores[role]
	if !ok {
		return fallbackPersona(role)
	}

	return loadPersonaFromDB(ctx, role, graphStore, factStore)
}

// LoadAllPersonas loads personas for all roles.
func (store *PersonaStore) LoadAllPersonas(ctx context.Context) map[agent.Role]Persona {
	personas := make(map[agent.Role]Persona, len(store.graphStores))

	for _, role := range agent.AllRoles() {
		personas[role] = store.LoadPersona(ctx, role)
	}

	return personas
}

// SaveChosenName stores an agent's chosen name in their memory DB as a
// high-confidence identity fact.
func (store *PersonaStore) SaveChosenName(ctx context.Context, role agent.Role, name string) error {
	graphStore, ok := store.graphStores[role]
	if !ok {
		return fmt.Errorf("no graph store for role %s", role)
	}

	factStore, ok := store.factStores[role]
	if !ok {
		return fmt.Errorf("no fact store for role %s", role)
	}

	entity, _, err := graphStore.FindOrCreateEntity(ctx, memory.EntityConcept, "identity", "this agent's personal identity")
	if err != nil {
		return fmt.Errorf("creating identity entity for %s: %w", role, err)
	}

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID:      entity.ID,
		Content:       fmt.Sprintf("my chosen name is %s", name),
		Type:          memory.FactPreference,
		Confidence:    1.0,
		Confirmations: 100,
	})
	if err != nil {
		return fmt.Errorf("storing name for %s: %w", role, err)
	}

	return nil
}

// HasChosenName checks whether an agent has already picked a name.
func (store *PersonaStore) HasChosenName(ctx context.Context, role agent.Role) bool {
	persona := store.LoadPersona(ctx, role)
	return persona.Name != string(role)
}

func loadPersonaFromDB(ctx context.Context, role agent.Role, graphStore *memory.GraphStore, factStore *memory.FactStore) Persona {
	entity, err := graphStore.FindEntityByName(ctx, memory.EntityConcept, "identity")
	if err != nil {
		return fallbackPersona(role)
	}

	facts, err := factStore.FactsByEntity(ctx, entity.ID)
	if err != nil {
		return fallbackPersona(role)
	}

	name := extractChosenName(facts, role)
	return Persona{
		Role:    role,
		Name:    name,
		IconURL: GenerateIdenticonURL(name),
	}
}

func extractChosenName(facts []memory.Fact, role agent.Role) string {
	for _, fact := range facts {
		if fact.Type != memory.FactPreference {
			continue
		}

		name := parseNameFromFact(fact.Content)
		if name != "" {
			return name
		}
	}

	return string(role)
}

func parseNameFromFact(content string) string {
	const prefix = "my chosen name is "
	if len(content) <= len(prefix) {
		return ""
	}

	if content[:len(prefix)] != prefix {
		return ""
	}

	return content[len(prefix):]
}

func fallbackPersona(role agent.Role) Persona {
	name := string(role)
	return Persona{
		Role:    role,
		Name:    name,
		IconURL: GenerateIdenticonURL(name),
	}
}

// GenerateIdenticonURL creates a unique avatar URL from a name using a
// hash-based identicon service.
func GenerateIdenticonURL(name string) string {
	hash := sha256.Sum256([]byte(name))
	return fmt.Sprintf("https://api.dicebear.com/9.x/identicon/svg?seed=%x", hash[:8])
}
