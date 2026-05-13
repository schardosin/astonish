package pgstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/config"
)

// PoolManager manages per-org connection pools.
//
// Each organization has its own PostgreSQL database and its own connection pool.
// The PoolManager lazily creates pools on first access and caches them.
// It also provides methods to configure connections for a specific request
// (setting search_path and session variables for RLS).
type PoolManager struct {
	mu          sync.RWMutex
	pools       map[string]*pgxpool.Pool // keyed by org slug
	platformDSN string
	pgCfg       config.PostgresConfig
}

// NewPoolManager creates a new pool manager.
func NewPoolManager(platformDSN string, pgCfg config.PostgresConfig) *PoolManager {
	return &PoolManager{
		pools:       make(map[string]*pgxpool.Pool),
		platformDSN: platformDSN,
		pgCfg:       pgCfg,
	}
}

// PlatformConn returns a single connection to the platform database.
// The caller is responsible for closing it.
func (pm *PoolManager) PlatformConn(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, pm.platformDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	return conn, nil
}

// PlatformPool returns (or lazily creates) a connection pool for the platform
// database. Use this for concurrent platform-level operations.
func (pm *PoolManager) PlatformPool(ctx context.Context) (*pgxpool.Pool, error) {
	return pm.getOrCreatePool(ctx, "_platform", pm.platformDSN)
}

// OrgPool returns (or lazily creates) a connection pool for the given
// organization's database.
func (pm *PoolManager) OrgPool(ctx context.Context, orgSlug string) (*pgxpool.Pool, error) {
	orgDSN, err := ReplaceDSNDatabase(pm.platformDSN, OrgDBName(pm.pgCfg.InstanceSuffix, orgSlug))
	if err != nil {
		return nil, fmt.Errorf("failed to derive org DSN for %s: %w", orgSlug, err)
	}
	return pm.getOrCreatePool(ctx, orgSlug, orgDSN)
}

// AcquireForRequest acquires a connection from the org pool and configures it
// for the given request context (user, team, search_path, RLS session vars).
//
// The caller MUST call release() when done to return the connection to the pool.
// The release function resets the search_path to prevent state leakage.
func (pm *PoolManager) AcquireForRequest(ctx context.Context, orgSlug, userID, teamSlug string) (conn *pgxpool.Conn, release func(), err error) {
	pool, err := pm.OrgPool(ctx, orgSlug)
	if err != nil {
		return nil, nil, err
	}

	c, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection from pool for org %s: %w", orgSlug, err)
	}

	// Build the search_path: personal → team → public
	// This means unqualified table names resolve personal-first, then team, then org-shared.
	searchPath := buildSearchPath(userID, teamSlug)

	// Set search_path and RLS session variables in a single batch
	batch := &pgx.Batch{}
	batch.Queue(fmt.Sprintf(`SET search_path TO %s`, searchPath))
	batch.Queue(`SELECT set_config('app.current_user', $1, true)`, userID)
	if teamSlug != "" {
		batch.Queue(`SELECT set_config('app.current_team', $1, true)`, teamSlug)
	}

	br := c.SendBatch(ctx, batch)
	// Read all results to ensure they completed successfully
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			br.Close()
			c.Release()
			return nil, nil, fmt.Errorf("failed to configure connection for request: %w", err)
		}
	}
	br.Close()

	// Release function resets search_path before returning to pool
	releaseFn := func() {
		// Reset search_path to default to prevent state leakage
		_, _ = c.Exec(context.Background(), `RESET search_path`)
		_, _ = c.Exec(context.Background(), `RESET ALL`)
		c.Release()
	}

	return c, releaseFn, nil
}

// Close closes all managed pools. Call this during shutdown.
func (pm *PoolManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for slug, pool := range pm.pools {
		pool.Close()
		delete(pm.pools, slug)
	}
}

// RemovePool closes and removes the pool for a specific org.
// Useful when decommissioning an organization.
func (pm *PoolManager) RemovePool(orgSlug string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pool, ok := pm.pools[orgSlug]; ok {
		pool.Close()
		delete(pm.pools, orgSlug)
	}
}

// getOrCreatePool returns an existing pool or creates a new one.
func (pm *PoolManager) getOrCreatePool(ctx context.Context, key, dsn string) (*pgxpool.Pool, error) {
	// Fast path: check with read lock
	pm.mu.RLock()
	if pool, ok := pm.pools[key]; ok {
		pm.mu.RUnlock()
		return pool, nil
	}
	pm.mu.RUnlock()

	// Slow path: create with write lock
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, ok := pm.pools[key]; ok {
		return pool, nil
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN for pool %s: %w", key, err)
	}

	poolCfg.MaxConns = int32(pm.pgCfg.GetMaxOpenConns())
	poolCfg.MinConns = int32(pm.pgCfg.GetMaxIdleConns())
	poolCfg.MaxConnLifetime = pm.pgCfg.GetConnMaxLifetime()
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool for %s: %w", key, err)
	}

	// Verify the connection works
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping pool for %s: %w", key, err)
	}

	pm.pools[key] = pool
	return pool, nil
}

// buildSearchPath constructs the PostgreSQL search_path for a request.
// Order: personal_{uid} → team_{slug} → public
func buildSearchPath(userID, teamSlug string) string {
	parts := make([]string, 0, 3)

	if userID != "" {
		parts = append(parts, pgx.Identifier{PersonalSchemaName(userID)}.Sanitize())
	}
	if teamSlug != "" {
		parts = append(parts, pgx.Identifier{TeamSchemaName(teamSlug)}.Sanitize())
	}
	parts = append(parts, "public")

	return fmt.Sprintf("%s", joinComma(parts))
}

// joinComma joins strings with ", ".
func joinComma(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
