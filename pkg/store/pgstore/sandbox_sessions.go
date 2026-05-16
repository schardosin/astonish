package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// baseTemplateUUID is the well-known UUID for the "@base" template,
// matching the seed migration (005_seed_base_template.sql). The K8s
// backend uses the literal "@base" string internally but the DB column
// is typed UUID, so we normalize at the persistence boundary.
const baseTemplateUUID = "a0000000-0000-4000-8000-000000000001"

// pgSandboxSessionStore backs store.SandboxSessionStore with the team-schema
// {schema}.sandbox_sessions table (migration team/002).
//
// Invariants enforced at this layer:
//   - Put inserts on first write and UPDATEs on subsequent ones, preserving
//     the original created_at (unlike filestore this is done in SQL rather
//     than in-memory). CreatedAt from the caller is ignored on replace.
//   - Delete is idempotent (no error when the row is absent) to match the
//     legacy pkg/sandbox.SessionRegistry.Remove contract.
//   - UpdateState / UpdatePorts / SetBaseDomain / SetPinned / SetUpperLayer
//     error out if the row is absent. This surfaces bookkeeping bugs.
//
// Ref-count discipline on upper_layer_id is the caller's responsibility
// today; the platform/004 ref-count trigger (pending) is the backstop.
type pgSandboxSessionStore struct {
	pool   *pgxpool.Pool
	schema string
}

// NewPGSandboxSessionStore returns a session store scoped to a team schema.
// Pass the team pool (cross-DB) and the concrete schema name.
func NewPGSandboxSessionStore(pool *pgxpool.Pool, schema string) store.SandboxSessionStore {
	return &pgSandboxSessionStore{pool: pool, schema: schema}
}

func (s *pgSandboxSessionStore) table() string {
	return pgx.Identifier{s.schema, "sandbox_sessions"}.Sanitize()
}

// nullableText converts an empty string to SQL NULL; nonempty strings are
// passed through. This keeps optional TEXT columns (container_name,
// upper_layer_id, base_domain, pod_name, node_name) NULL rather than "".
func nullableText(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// nullableUUID is the same as nullableText but used to flag the intent for
// UUID columns (created_by). Storing "" as NULL avoids a parse error since
// the column is typed UUID.
func nullableUUID(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// encodePorts serializes a []int to the JSONB representation stored in the
// exposed_ports column. Nil/empty slices become []::jsonb so the column's
// NOT NULL constraint is satisfied.
func encodePorts(ports []int) ([]byte, error) {
	if len(ports) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(ports)
}

func decodePorts(raw []byte) ([]int, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []int
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode exposed_ports: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// scanColumns is the column list used by every SELECT in this store.
// Ordered to match scanRow.
const scanColumns = `id, chat_session_id, backend, container_name, template_id::text,
  upper_layer_id, state, pod_name, node_name, exposed_ports,
  base_domain, pinned, COALESCE(created_by::text, ''),
  created_at, updated_at, last_active_at`

// scanRow reads one row of scanColumns from the given row-like into a
// *store.SandboxSession. pgx.Rows and pgx.Row both satisfy the Scan method
// so we accept an interface.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(r rowScanner) (*store.SandboxSession, error) {
	sess := &store.SandboxSession{}
	var containerName, upperLayerID, podName, nodeName, baseDomain *string
	var portsRaw []byte
	err := r.Scan(
		&sess.SessionID,
		&sess.ChatSessionID,
		&sess.Backend,
		&containerName,
		&sess.TemplateID,
		&upperLayerID,
		&sess.State,
		&podName,
		&nodeName,
		&portsRaw,
		&baseDomain,
		&sess.Pinned,
		&sess.CreatedBy,
		&sess.CreatedAt,
		&sess.UpdatedAt,
		&sess.LastActiveAt,
	)
	if err != nil {
		return nil, err
	}
	if containerName != nil {
		sess.ContainerName = *containerName
	}
	if upperLayerID != nil {
		sess.UpperLayerID = *upperLayerID
	}
	if podName != nil {
		sess.PodName = *podName
	}
	if nodeName != nil {
		sess.NodeName = *nodeName
	}
	if baseDomain != nil {
		sess.BaseDomain = *baseDomain
	}
	ports, err := decodePorts(portsRaw)
	if err != nil {
		return nil, err
	}
	sess.ExposedPorts = ports
	return sess, nil
}

// Put inserts or updates the row. On conflict, created_at is preserved and
// updated_at/last_active_at are set to now().
func (s *pgSandboxSessionStore) Put(ctx context.Context, sess *store.SandboxSession) error {
	if sess == nil {
		return errors.New("sandbox session is nil")
	}
	if sess.SessionID == "" {
		return errors.New("sandbox session: SessionID is required")
	}
	if sess.TemplateID == "" {
		return errors.New("sandbox session: TemplateID is required")
	}
	chatID := sess.ChatSessionID
	if chatID == "" {
		chatID = sess.SessionID
	}
	backend := sess.Backend
	if backend == "" {
		backend = "incus"
	}
	state := sess.State
	if state == "" {
		state = store.SandboxSessionStateCreating
	}
	// Normalize "@base" literal to the well-known UUID for the DB column
	// (template_id is typed UUID; the K8s backend uses "@base" in-memory).
	templateID := sess.TemplateID
	if templateID == "@base" {
		templateID = baseTemplateUUID
	}
	ports, err := encodePorts(sess.ExposedPorts)
	if err != nil {
		return err
	}

	q := fmt.Sprintf(`INSERT INTO %s
	  (id, chat_session_id, backend, container_name, template_id, upper_layer_id,
	   state, pod_name, node_name, exposed_ports, base_domain, pinned,
	   created_by, created_at, updated_at, last_active_at)
	 VALUES ($1, $2, $3, $4, $5::uuid, $6, $7, $8, $9, $10::jsonb, $11, $12,
	         $13, COALESCE($14, now()), now(), COALESCE($15, now()))
	 ON CONFLICT (id) DO UPDATE SET
	   chat_session_id = EXCLUDED.chat_session_id,
	   backend         = EXCLUDED.backend,
	   container_name  = EXCLUDED.container_name,
	   template_id     = EXCLUDED.template_id,
	   upper_layer_id  = EXCLUDED.upper_layer_id,
	   state           = EXCLUDED.state,
	   pod_name        = EXCLUDED.pod_name,
	   node_name       = EXCLUDED.node_name,
	   exposed_ports   = EXCLUDED.exposed_ports,
	   base_domain     = EXCLUDED.base_domain,
	   pinned          = EXCLUDED.pinned,
	   created_by      = EXCLUDED.created_by,
	   updated_at      = now(),
	   last_active_at  = EXCLUDED.last_active_at`, s.table())

	var createdAt, lastActiveAt any
	if !sess.CreatedAt.IsZero() {
		createdAt = sess.CreatedAt
	}
	if !sess.LastActiveAt.IsZero() {
		lastActiveAt = sess.LastActiveAt
	}

	_, err = s.pool.Exec(ctx, q,
		sess.SessionID,
		chatID,
		backend,
		nullableText(sess.ContainerName),
		templateID,
		nullableText(sess.UpperLayerID),
		string(state),
		nullableText(sess.PodName),
		nullableText(sess.NodeName),
		ports,
		nullableText(sess.BaseDomain),
		sess.Pinned,
		nullableUUID(sess.CreatedBy),
		createdAt,
		lastActiveAt,
	)
	if err != nil {
		return fmt.Errorf("upsert sandbox_session %s: %w", sess.SessionID, err)
	}
	return nil
}

func (s *pgSandboxSessionStore) Get(ctx context.Context, sessionID string) (*store.SandboxSession, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE id = $1`, scanColumns, s.table()),
		sessionID,
	)
	sess, err := scanRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return sess, nil
}

func (s *pgSandboxSessionStore) GetByContainerName(ctx context.Context, containerName string) (*store.SandboxSession, error) {
	if containerName == "" {
		return nil, nil
	}
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE container_name = $1`, scanColumns, s.table()),
		containerName,
	)
	sess, err := scanRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return sess, nil
}

func (s *pgSandboxSessionStore) List(ctx context.Context, filter store.SandboxSessionFilter) ([]*store.SandboxSession, error) {
	var (
		where []string
		args  []any
	)
	if filter.State != "" {
		args = append(args, string(filter.State))
		where = append(where, fmt.Sprintf("state = $%d", len(args)))
	}
	if filter.CreatedBy != "" {
		args = append(args, filter.CreatedBy)
		where = append(where, fmt.Sprintf("created_by = $%d::uuid", len(args)))
	}
	if filter.Pinned != nil {
		args = append(args, *filter.Pinned)
		where = append(where, fmt.Sprintf("pinned = $%d", len(args)))
	}
	if filter.ContainerName != "" {
		args = append(args, filter.ContainerName)
		where = append(where, fmt.Sprintf("container_name = $%d", len(args)))
	}
	q := fmt.Sprintf(`SELECT %s FROM %s`, scanColumns, s.table())
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.SandboxSession
	for rows.Next() {
		sess, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// Delete is idempotent; it does not error when the row is absent.
func (s *pgSandboxSessionStore) Delete(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.table()),
		sessionID,
	)
	return err
}

func (s *pgSandboxSessionStore) UpdateState(ctx context.Context, sessionID string, state store.SandboxSessionState) error {
	ct, err := s.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET state = $2, updated_at = now(), last_active_at = now() WHERE id = $1`, s.table()),
		sessionID, string(state),
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	return nil
}

func (s *pgSandboxSessionStore) UpdatePorts(ctx context.Context, sessionID string, ports []int) error {
	raw, err := encodePorts(ports)
	if err != nil {
		return err
	}
	ct, err := s.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET exposed_ports = $2::jsonb, updated_at = now() WHERE id = $1`, s.table()),
		sessionID, raw,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	return nil
}

func (s *pgSandboxSessionStore) SetBaseDomain(ctx context.Context, sessionID, baseDomain string) error {
	ct, err := s.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET base_domain = $2, updated_at = now() WHERE id = $1`, s.table()),
		sessionID, nullableText(baseDomain),
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	return nil
}

func (s *pgSandboxSessionStore) SetPinned(ctx context.Context, sessionID string, pinned bool) error {
	ct, err := s.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET pinned = $2, updated_at = now() WHERE id = $1`, s.table()),
		sessionID, pinned,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	return nil
}

// SetUpperLayer associates or clears the evicted-upper layer. Ref-count
// adjustments on platform.sandbox_layers must happen in a surrounding
// transaction; see architecture §5.12 and §7.5. The platform/004 trigger is
// the backstop if the application forgets.
func (s *pgSandboxSessionStore) SetUpperLayer(ctx context.Context, sessionID, upperLayerID string) error {
	ct, err := s.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET upper_layer_id = $2, updated_at = now() WHERE id = $1`, s.table()),
		sessionID, nullableText(upperLayerID),
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	return nil
}

// Compile-time assertion.
var _ store.SandboxSessionStore = (*pgSandboxSessionStore)(nil)
