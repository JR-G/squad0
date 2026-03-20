package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EntityType enumerates the kinds of entity tracked in the knowledge graph.
type EntityType string

const (
	// EntityModule represents a code module or package.
	EntityModule EntityType = "module"
	// EntityFile represents a source file.
	EntityFile EntityType = "file"
	// EntityPattern represents a code pattern or convention.
	EntityPattern EntityType = "pattern"
	// EntityTool represents a tool or dependency.
	EntityTool EntityType = "tool"
	// EntityConcept represents an abstract concept or domain term.
	EntityConcept EntityType = "concept"
)

// RelationType enumerates the kinds of edges between entities.
type RelationType string

const (
	// RelationDependsOn indicates the source depends on the target.
	RelationDependsOn RelationType = "depends_on"
	// RelationBreaksWhen indicates the source breaks when the target changes.
	RelationBreaksWhen RelationType = "breaks_when"
	// RelationPairsWith indicates the source works well with the target.
	RelationPairsWith RelationType = "pairs_with"
	// RelationReplaces indicates the source replaces the target.
	RelationReplaces RelationType = "replaces"
)

// Entity represents a named thing the agent knows about.
type Entity struct {
	ID        int64
	Type      EntityType
	Name      string
	Summary   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Relationship represents a directed edge between two entities.
type Relationship struct {
	ID          int64
	SourceID    int64
	TargetID    int64
	Type        RelationType
	Description string
	ValidFrom   time.Time
	ValidUntil  *time.Time
	Confidence  float64
}

// GraphStore provides CRUD operations for entities and relationships.
type GraphStore struct {
	db *DB
}

// NewGraphStore creates a GraphStore backed by the given database.
func NewGraphStore(db *DB) *GraphStore {
	return &GraphStore{db: db}
}

// CreateEntity inserts a new entity and returns its ID.
func (store *GraphStore) CreateEntity(ctx context.Context, entity Entity) (int64, error) {
	result, err := store.db.RawDB().ExecContext(ctx,
		`INSERT INTO entities (entity_type, name, summary) VALUES (?, ?, ?)`,
		string(entity.Type), entity.Name, entity.Summary,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting entity: %w", err)
	}

	return result.LastInsertId()
}

// GetEntity retrieves an entity by ID.
func (store *GraphStore) GetEntity(ctx context.Context, id int64) (Entity, error) {
	var entity Entity
	err := store.db.RawDB().QueryRowContext(ctx,
		`SELECT id, entity_type, name, summary, created_at, updated_at FROM entities WHERE id = ?`, id,
	).Scan(&entity.ID, &entity.Type, &entity.Name, &entity.Summary, &entity.CreatedAt, &entity.UpdatedAt)
	if err != nil {
		return Entity{}, fmt.Errorf("getting entity %d: %w", id, err)
	}

	return entity, nil
}

// FindEntityByName retrieves an entity by type and name. Returns
// sql.ErrNoRows if not found.
func (store *GraphStore) FindEntityByName(ctx context.Context, entityType EntityType, name string) (Entity, error) {
	var entity Entity
	err := store.db.RawDB().QueryRowContext(ctx,
		`SELECT id, entity_type, name, summary, created_at, updated_at FROM entities WHERE entity_type = ? AND name = ?`,
		string(entityType), name,
	).Scan(&entity.ID, &entity.Type, &entity.Name, &entity.Summary, &entity.CreatedAt, &entity.UpdatedAt)
	if err != nil {
		return Entity{}, fmt.Errorf("finding entity %s/%s: %w", entityType, name, err)
	}

	return entity, nil
}

// FindOrCreateEntity retrieves an entity by type and name, creating it if
// it does not exist. Returns the entity and whether it was newly created.
func (store *GraphStore) FindOrCreateEntity(ctx context.Context, entityType EntityType, name, summary string) (Entity, bool, error) {
	entity, err := store.FindEntityByName(ctx, entityType, name)
	if err == nil {
		return entity, false, nil
	}

	if !isNotFound(err) {
		return Entity{}, false, err
	}

	id, err := store.CreateEntity(ctx, Entity{Type: entityType, Name: name, Summary: summary})
	if err != nil {
		return Entity{}, false, err
	}

	entity, err = store.GetEntity(ctx, id)
	if err != nil {
		return Entity{}, false, err
	}

	return entity, true, nil
}

// UpdateEntitySummary updates the summary text and updated_at timestamp
// for the given entity.
func (store *GraphStore) UpdateEntitySummary(ctx context.Context, id int64, summary string) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE entities SET summary = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		summary, id,
	)
	if err != nil {
		return fmt.Errorf("updating entity %d summary: %w", id, err)
	}

	return nil
}

// CreateRelationship inserts a new relationship and returns its ID.
func (store *GraphStore) CreateRelationship(ctx context.Context, rel Relationship) (int64, error) {
	result, err := store.db.RawDB().ExecContext(ctx,
		`INSERT INTO relationships (source_id, target_id, relation_type, description, confidence) VALUES (?, ?, ?, ?, ?)`,
		rel.SourceID, rel.TargetID, string(rel.Type), rel.Description, rel.Confidence,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting relationship: %w", err)
	}

	return result.LastInsertId()
}

// InvalidateRelationship sets the valid_until timestamp on a relationship,
// marking it as no longer current.
func (store *GraphStore) InvalidateRelationship(ctx context.Context, id int64) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE relationships SET valid_until = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("invalidating relationship %d: %w", id, err)
	}

	return nil
}

// RelatedEntities returns all entities connected to the given entity ID
// by valid (non-expired) relationships, up to maxDepth hops.
func (store *GraphStore) RelatedEntities(ctx context.Context, entityID int64, maxDepth int) ([]Entity, error) {
	visited := map[int64]bool{entityID: true}
	frontier := []int64{entityID}

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var nextFrontier []int64

		for _, currentID := range frontier {
			neighbours, err := store.directNeighbours(ctx, currentID)
			if err != nil {
				return nil, err
			}

			for _, neighbourID := range neighbours {
				if visited[neighbourID] {
					continue
				}
				visited[neighbourID] = true
				nextFrontier = append(nextFrontier, neighbourID)
			}
		}

		frontier = nextFrontier
	}

	delete(visited, entityID)

	entities := make([]Entity, 0, len(visited))
	for id := range visited {
		entity, err := store.GetEntity(ctx, id)
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

func (store *GraphStore) directNeighbours(ctx context.Context, entityID int64) ([]int64, error) {
	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT CASE WHEN source_id = ? THEN target_id ELSE source_id END
		 FROM relationships
		 WHERE (source_id = ? OR target_id = ?) AND valid_until IS NULL`,
		entityID, entityID, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying neighbours of entity %d: %w", entityID, err)
	}
	defer func() { _ = rows.Close() }()

	var neighbours []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning neighbour: %w", err)
		}
		neighbours = append(neighbours, id)
	}

	return neighbours, rows.Err()
}

func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
