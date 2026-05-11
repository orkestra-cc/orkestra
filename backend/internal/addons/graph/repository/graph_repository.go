package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
	"github.com/orkestra/backend/internal/addons/graph/models"
	"github.com/orkestra/backend/internal/shared/iface"
)

// GraphRepository defines the interface for graph database operations
type GraphRepository interface {
	ExecuteRead(ctx context.Context, database string, cypher string, params map[string]interface{}) (*iface.QueryResult, error)
	ExecuteWrite(ctx context.Context, database string, cypher string, params map[string]interface{}) (*iface.QueryResult, error)
	// ExecuteAutoCommit runs a query as an implicit/auto-commit transaction.
	// Required for Memgraph storage commands (CREATE INDEX, SHOW INDEX INFO, etc.)
	ExecuteAutoCommit(ctx context.Context, database string, cypher string, params map[string]interface{}) error
	ListDatabases(ctx context.Context) ([]models.DatabaseInfo, error)
	GetSchema(ctx context.Context, database string) (*models.SchemaInfo, error)
	VerifyConnectivity(ctx context.Context) error
}

type graphRepository struct {
	driver          neo4j.DriverWithContext
	defaultDatabase string
}

// NewGraphRepository creates a new GraphRepository
func NewGraphRepository(driver neo4j.DriverWithContext, defaultDatabase string) GraphRepository {
	return &graphRepository{
		driver:          driver,
		defaultDatabase: defaultDatabase,
	}
}

func (r *graphRepository) resolveDatabase(database string) string {
	if database != "" {
		return database
	}
	return r.defaultDatabase
}

func (r *graphRepository) ExecuteRead(ctx context.Context, database string, cypher string, params map[string]interface{}) (*iface.QueryResult, error) {
	db := r.resolveDatabase(database)
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: db, AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	start := time.Now()
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		res, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		return collectResults(ctx, res)
	})
	if err != nil {
		return nil, fmt.Errorf("read query failed: %w", err)
	}

	qr := result.(*iface.QueryResult)
	qr.Metadata.ExecutionTimeMs = time.Since(start).Milliseconds()
	return qr, nil
}

func (r *graphRepository) ExecuteWrite(ctx context.Context, database string, cypher string, params map[string]interface{}) (*iface.QueryResult, error) {
	db := r.resolveDatabase(database)
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: db, AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	start := time.Now()
	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		res, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		return collectResults(ctx, res)
	})
	if err != nil {
		return nil, fmt.Errorf("write query failed: %w", err)
	}

	qr := result.(*iface.QueryResult)
	qr.Metadata.ExecutionTimeMs = time.Since(start).Milliseconds()
	return qr, nil
}

func (r *graphRepository) ExecuteAutoCommit(ctx context.Context, database string, cypher string, params map[string]interface{}) error {
	db := r.resolveDatabase(database)
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: db, AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("auto-commit query failed: %w", err)
	}
	return nil
}

func (r *graphRepository) ListDatabases(ctx context.Context) ([]models.DatabaseInfo, error) {
	// Memgraph is a single-database system — return a synthetic entry
	db := r.defaultDatabase
	if db == "" {
		db = "memgraph"
	}
	return []models.DatabaseInfo{{
		Name:          db,
		Address:       "local",
		CurrentStatus: "online",
		Default:       true,
		Home:          true,
	}}, nil
}

func (r *graphRepository) GetSchema(ctx context.Context, database string) (*models.SchemaInfo, error) {
	db := r.resolveDatabase(database)
	schema := &models.SchemaInfo{}

	// Use a managed transaction for standard Cypher queries (labels, relationships, counts)
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: db, AccessMode: neo4j.AccessModeRead})
	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		// Get labels
		res, err := tx.Run(ctx, "MATCH (n) UNWIND labels(n) AS label RETURN DISTINCT label", nil)
		if err != nil {
			return nil, err
		}
		for res.Next(ctx) {
			name, _ := res.Record().Get("label")
			label := models.LabelInfo{Name: name.(string)}

			countRes, err := tx.Run(ctx, fmt.Sprintf("MATCH (n:`%s`) RETURN count(n) AS cnt", name.(string)), nil)
			if err == nil && countRes.Next(ctx) {
				if cnt, ok := countRes.Record().Get("cnt"); ok {
					label.Count, _ = cnt.(int64)
				}
			}

			propRes, err := tx.Run(ctx, fmt.Sprintf("MATCH (n:`%s`) UNWIND keys(n) AS key RETURN DISTINCT key", name.(string)), nil)
			if err == nil {
				for propRes.Next(ctx) {
					if key, ok := propRes.Record().Get("key"); ok {
						label.Properties = append(label.Properties, key.(string))
					}
				}
			}
			schema.Labels = append(schema.Labels, label)
		}

		// Get relationship types
		res, err = tx.Run(ctx, "MATCH ()-[r]->() RETURN DISTINCT type(r) AS relationshipType", nil)
		if err != nil {
			return nil, err
		}
		for res.Next(ctx) {
			name, _ := res.Record().Get("relationshipType")
			relType := models.RelTypeInfo{Name: name.(string)}

			countRes, err := tx.Run(ctx, fmt.Sprintf("MATCH ()-[r:`%s`]->() RETURN count(r) AS cnt", name.(string)), nil)
			if err == nil && countRes.Next(ctx) {
				if cnt, ok := countRes.Record().Get("cnt"); ok {
					relType.Count, _ = cnt.(int64)
				}
			}

			propRes, err := tx.Run(ctx, fmt.Sprintf("MATCH ()-[r:`%s`]->() UNWIND keys(r) AS key RETURN DISTINCT key", name.(string)), nil)
			if err == nil {
				for propRes.Next(ctx) {
					if key, ok := propRes.Record().Get("key"); ok {
						relType.Properties = append(relType.Properties, key.(string))
					}
				}
			}
			schema.RelationshipTypes = append(schema.RelationshipTypes, relType)
		}

		// Get total counts
		countRes, err := tx.Run(ctx, "MATCH (n) RETURN count(n) AS nodeCount", nil)
		if err == nil && countRes.Next(ctx) {
			if cnt, ok := countRes.Record().Get("nodeCount"); ok {
				schema.NodeCount, _ = cnt.(int64)
			}
		}

		countRes, err = tx.Run(ctx, "MATCH ()-[r]->() RETURN count(r) AS relCount", nil)
		if err == nil && countRes.Next(ctx) {
			if cnt, ok := countRes.Record().Get("relCount"); ok {
				schema.RelationshipCount, _ = cnt.(int64)
			}
		}

		return nil, nil
	})
	session.Close(ctx)
	if err != nil {
		return nil, err
	}

	// Memgraph requires SHOW INDEX INFO / SHOW CONSTRAINT INFO to run as auto-commit
	// transactions (implicit transactions), not inside managed transactions.
	r.getSchemaIndexes(ctx, db, schema)
	r.getSchemaConstraints(ctx, db, schema)

	return schema, nil
}

// getSchemaIndexes fetches index info using an auto-commit transaction
func (r *graphRepository) getSchemaIndexes(ctx context.Context, db string, schema *models.SchemaInfo) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: db, AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	res, err := session.Run(ctx, "SHOW INDEX INFO", nil)
	if err != nil {
		return
	}
	for res.Next(ctx) {
		record := res.Record()
		idx := models.IndexInfo{State: "ONLINE"}
		if v, ok := record.Get("index type"); ok {
			idx.Type, _ = v.(string)
		}
		if v, ok := record.Get("label"); ok {
			if label, ok := v.(string); ok {
				idx.Labels = []string{label}
			}
		}
		if v, ok := record.Get("property"); ok {
			if prop, ok := v.(string); ok {
				idx.Properties = []string{prop}
			}
		}
		if len(idx.Labels) > 0 && len(idx.Properties) > 0 {
			idx.Name = fmt.Sprintf("%s_%s", idx.Labels[0], idx.Properties[0])
		} else if len(idx.Labels) > 0 {
			idx.Name = idx.Labels[0]
		}
		schema.Indexes = append(schema.Indexes, idx)
	}
}

// getSchemaConstraints fetches constraint info using an auto-commit transaction
func (r *graphRepository) getSchemaConstraints(ctx context.Context, db string, schema *models.SchemaInfo) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: db, AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	res, err := session.Run(ctx, "SHOW CONSTRAINT INFO", nil)
	if err != nil {
		return
	}
	for res.Next(ctx) {
		record := res.Record()
		c := models.ConstraintInfo{}
		if v, ok := record.Get("constraint type"); ok {
			c.Type, _ = v.(string)
		}
		if v, ok := record.Get("label"); ok {
			if label, ok := v.(string); ok {
				c.Labels = []string{label}
			}
		}
		if v, ok := record.Get("properties"); ok {
			c.Properties = toStringSlice(v)
		}
		if len(c.Labels) > 0 {
			c.Name = fmt.Sprintf("%s_%s", c.Type, c.Labels[0])
		}
		schema.Constraints = append(schema.Constraints, c)
	}
}

func (r *graphRepository) VerifyConnectivity(ctx context.Context) error {
	return r.driver.VerifyConnectivity(ctx)
}

// collectResults processes a result set into a QueryResult
func collectResults(ctx context.Context, res neo4j.ResultWithContext) (*iface.QueryResult, error) {
	qr := &iface.QueryResult{
		Graph: &iface.GraphData{},
	}

	// Track unique nodes/rels for graph extraction
	seenNodes := make(map[int64]bool)
	seenRels := make(map[int64]bool)

	keys, err := res.Keys()
	if err != nil {
		return nil, err
	}
	qr.Columns = keys

	for res.Next(ctx) {
		record := res.Record()
		row := make(map[string]interface{})

		for _, key := range keys {
			val, _ := record.Get(key)
			row[key] = marshalValue(val, qr.Graph, seenNodes, seenRels)
		}
		qr.Rows = append(qr.Rows, row)
	}

	if err := res.Err(); err != nil {
		return nil, err
	}

	// Extract counters from summary
	summary, err := res.Consume(ctx)
	if err == nil && summary.Counters().ContainsUpdates() {
		c := summary.Counters()
		qr.Metadata.NodesCreated = c.NodesCreated()
		qr.Metadata.NodesDeleted = c.NodesDeleted()
		qr.Metadata.RelationshipsCreated = c.RelationshipsCreated()
		qr.Metadata.RelationshipsDeleted = c.RelationshipsDeleted()
		qr.Metadata.PropertiesSet = c.PropertiesSet()
		qr.Metadata.LabelsAdded = c.LabelsAdded()
		qr.Metadata.LabelsRemoved = c.LabelsRemoved()
		qr.Metadata.ContainsUpdates = true
	}

	qr.Metadata.ResultCount = len(qr.Rows)

	// Clean up empty graph data
	if len(qr.Graph.Nodes) == 0 && len(qr.Graph.Relationships) == 0 {
		qr.Graph = nil
	}

	return qr, nil
}

// marshalValue converts Bolt driver types to JSON-serializable values
// and extracts graph elements for visualization
func marshalValue(val interface{}, graph *iface.GraphData, seenNodes map[int64]bool, seenRels map[int64]bool) interface{} {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case dbtype.Node:
		node := iface.GraphNode{
			ID:         v.Id,
			ElementID:  v.ElementId,
			Labels:     v.Labels,
			Properties: sanitizeProps(v.Props),
		}
		if graph != nil && !seenNodes[v.Id] {
			seenNodes[v.Id] = true
			graph.Nodes = append(graph.Nodes, node)
		}
		return node

	case dbtype.Relationship:
		rel := iface.GraphRelationship{
			ID:             v.Id,
			ElementID:      v.ElementId,
			Type:           v.Type,
			StartNodeID:    v.StartId,
			EndNodeID:      v.EndId,
			StartElementID: v.StartElementId,
			EndElementID:   v.EndElementId,
			Properties:     sanitizeProps(v.Props),
		}
		if graph != nil && !seenRels[v.Id] {
			seenRels[v.Id] = true
			graph.Relationships = append(graph.Relationships, rel)
		}
		return rel

	case dbtype.Path:
		// Extract all nodes and relationships from the path
		pathData := map[string]interface{}{
			"_type": "path",
		}
		var nodes []interface{}
		for _, n := range v.Nodes {
			nodes = append(nodes, marshalValue(n, graph, seenNodes, seenRels))
		}
		pathData["nodes"] = nodes

		var rels []interface{}
		for _, r := range v.Relationships {
			rels = append(rels, marshalValue(r, graph, seenNodes, seenRels))
		}
		pathData["relationships"] = rels
		return pathData

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = marshalValue(item, graph, seenNodes, seenRels)
		}
		return result

	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, item := range v {
			result[k] = marshalValue(item, graph, seenNodes, seenRels)
		}
		return result

	default:
		return v
	}
}

// sanitizeProps converts property maps to clean maps, handling special types
func sanitizeProps(props map[string]interface{}) map[string]interface{} {
	if props == nil {
		return make(map[string]interface{})
	}
	result := make(map[string]interface{}, len(props))
	for k, v := range props {
		result[k] = marshalValue(v, nil, nil, nil)
	}
	return result
}

// toStringSlice converts an interface{} to a string slice
func toStringSlice(val interface{}) []string {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return v
	default:
		return nil
	}
}
