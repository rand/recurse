package hypergraph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStore_InMemory(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	assert.NotNil(t, store.DB())
	assert.Empty(t, store.Path())
}

func TestNewStore_File(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(Options{
		Path:              dbPath,
		CreateIfNotExists: true,
	})
	require.NoError(t, err)
	defer store.Close()

	assert.Equal(t, dbPath, store.Path())

	// Verify file was created
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}

func TestNewStore_NestedDir(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nested", "deep", "test.db")

	store, err := NewStore(Options{
		Path:              dbPath,
		CreateIfNotExists: true,
	})
	require.NoError(t, err)
	defer store.Close()

	// Verify nested directories were created
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}

func TestStore_SchemaCreation(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Verify tables exist by querying them
	tables := []string{"nodes", "hyperedges", "membership", "decisions", "evolution_log", "projects", "node_projects"}

	for _, table := range tables {
		var count int
		err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
		assert.NoError(t, err, "table %s should exist", table)
	}
}

func TestStore_Stats_Empty(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	stats, err := store.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(0), stats.NodeCount)
	assert.Equal(t, int64(0), stats.HyperedgeCount)
	assert.Empty(t, stats.NodesByTier)
	assert.Empty(t, stats.NodesByType)
}

func TestStore_Stats_WithData(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Insert test data
	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO nodes (id, type, subtype, content, tier) VALUES
		('n1', 'entity', 'file', 'test.go', 'task'),
		('n2', 'entity', 'function', 'TestFunc', 'task'),
		('n3', 'fact', NULL, 'Some fact', 'session'),
		('n4', 'experience', NULL, 'User preference', 'longterm')
	`)
	require.NoError(t, err)

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO hyperedges (id, type, label) VALUES
		('e1', 'relation', 'contains'),
		('e2', 'causation', 'causes')
	`)
	require.NoError(t, err)

	stats, err := store.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(4), stats.NodeCount)
	assert.Equal(t, int64(2), stats.HyperedgeCount)

	// Check tier distribution
	assert.Equal(t, int64(2), stats.NodesByTier["task"])
	assert.Equal(t, int64(1), stats.NodesByTier["session"])
	assert.Equal(t, int64(1), stats.NodesByTier["longterm"])

	// Check type distribution
	assert.Equal(t, int64(2), stats.NodesByType["entity"])
	assert.Equal(t, int64(1), stats.NodesByType["fact"])
	assert.Equal(t, int64(1), stats.NodesByType["experience"])
}

func TestStore_WithTx_Commit(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	err = store.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO nodes (id, type, content) VALUES ('tx1', 'fact', 'test')`)
		return err
	})
	require.NoError(t, err)

	// Verify data was committed
	var count int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes WHERE id = 'tx1'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestStore_WithTx_Rollback(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	err = store.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO nodes (id, type, content) VALUES ('tx2', 'fact', 'test')`)
		if err != nil {
			return err
		}
		return fmt.Errorf("simulated error")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated error")

	// Verify data was rolled back
	var count int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes WHERE id = 'tx2'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestStore_ForeignKeyConstraints(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Try to insert membership without valid node - should fail
	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO hyperedges (id, type, label) VALUES ('e1', 'relation', 'test')
	`)
	require.NoError(t, err)

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO membership (hyperedge_id, node_id, role) VALUES ('e1', 'nonexistent', 'subject')
	`)
	assert.Error(t, err, "foreign key constraint should prevent invalid node reference")
}

func TestStore_CheckConstraints(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Invalid node type
	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO nodes (id, type, content) VALUES ('n1', 'invalid_type', 'test')
	`)
	assert.Error(t, err, "check constraint should prevent invalid node type")

	// Invalid tier
	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO nodes (id, type, content, tier) VALUES ('n2', 'fact', 'test', 'invalid_tier')
	`)
	assert.Error(t, err, "check constraint should prevent invalid tier")

	// Invalid confidence (out of range)
	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO nodes (id, type, content, confidence) VALUES ('n3', 'fact', 'test', 1.5)
	`)
	assert.Error(t, err, "check constraint should prevent invalid confidence")
}

func TestStore_CascadeDelete(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create node and hyperedge with membership
	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO nodes (id, type, content) VALUES ('n1', 'fact', 'test')
	`)
	require.NoError(t, err)

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO hyperedges (id, type, label) VALUES ('e1', 'relation', 'test')
	`)
	require.NoError(t, err)

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO membership (hyperedge_id, node_id, role) VALUES ('e1', 'n1', 'subject')
	`)
	require.NoError(t, err)

	// Delete node - membership should be cascade deleted
	_, err = store.DB().ExecContext(ctx, `DELETE FROM nodes WHERE id = 'n1'`)
	require.NoError(t, err)

	var count int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM membership WHERE node_id = 'n1'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "membership should be cascade deleted")
}
