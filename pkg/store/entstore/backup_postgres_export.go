package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/SAP/astonish/pkg/backup"
)

type postgresLogicalDB struct {
	DBName      string
	Schema      string
	ArchiveDir  string
	Scope       backup.Scope
	ScopeName   string
	Description string
}

func (s *Store) ExportPostgresLogicalBackup(ctx context.Context, archivePath string, opts PlatformBackupExportOptions) error {
	if s.dialect != DialectPostgres {
		return fmt.Errorf("postgres logical backup requires postgres backend, got %s", s.dialect)
	}
	manifest := backup.NewManifest("postgres", backupModeLogical, []backup.Scope{{Kind: "platform"}})
	manifest.Features = append(manifest.Features, "postgres-logical-row-export")
	if opts.RedactSecrets {
		manifest.Features = append(manifest.Features, "redacted-secrets")
	}
	manifest.SchemaVersions = map[string]backup.SchemaVersion{}

	writer, err := backup.Create(archivePath, backup.WriterOptions{Compression: opts.Compression})
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = writer.Close()
		}
	}()

	dbs, err := s.discoverPostgresLogicalDBs(ctx)
	if err != nil {
		return err
	}
	for _, db := range dbs {
		if !backupScopeSelected(db.Scope, opts) {
			continue
		}
		if !scopeInManifest(manifest.Scopes, db.Scope) {
			manifest.Scopes = append(manifest.Scopes, db.Scope)
		}
		if err := s.exportPostgresDBRows(ctx, writer, &manifest, db, opts); err != nil {
			return err
		}
	}
	manifest.Scopes = sortedManifestScopes(manifest.Scopes)

	if err := writer.CloseWithManifest(manifest); err != nil {
		return err
	}
	closed = true
	return nil
}

func (s *Store) discoverPostgresLogicalDBs(ctx context.Context) ([]postgresLogicalDB, error) {
	dbs := []postgresLogicalDB{{
		Schema:      "public",
		ArchiveDir:  "platform",
		Scope:       backup.Scope{Kind: "platform"},
		ScopeName:   "platform",
		Description: "platform",
	}}

	orgs, err := s.Organizations().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations for postgres backup: %w", err)
	}
	for _, org := range orgs {
		orgSlug := org.Slug
		orgDBName := org.DBName
		if orgDBName == "" {
			orgDBName = s.orgDBName(orgSlug)
		}
		dbs = append(dbs, postgresLogicalDB{
			DBName:      orgDBName,
			Schema:      "public",
			ArchiveDir:  archiveJoin("orgs", orgSlug, "org"),
			Scope:       backup.Scope{Kind: "org", OrgSlug: orgSlug},
			ScopeName:   "org:" + orgSlug,
			Description: "org " + orgSlug,
		})

		orgStore, err := s.ForOrg(orgSlug)
		if err != nil {
			return nil, fmt.Errorf("open org %s for postgres backup discovery: %w", orgSlug, err)
		}
		teams, err := orgStore.Teams().ListTeams(ctx)
		if err != nil {
			return nil, fmt.Errorf("list teams for org %s backup: %w", orgSlug, err)
		}
		for _, team := range teams {
			dbs = append(dbs, postgresLogicalDB{
				DBName:      orgDBName,
				Schema:      teamSchemaName(team.Slug),
				ArchiveDir:  archiveJoin("orgs", orgSlug, "teams", team.Slug),
				Scope:       backup.Scope{Kind: "team", OrgSlug: orgSlug, TeamSlug: team.Slug},
				ScopeName:   "team:" + orgSlug + ":" + team.Slug,
				Description: "team " + orgSlug + "/" + team.Slug,
			})
		}

		users, err := s.Users().ListByOrg(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("list users for org %s backup: %w", orgSlug, err)
		}
		for _, user := range users {
			dbs = append(dbs, postgresLogicalDB{
				DBName:      orgDBName,
				Schema:      personalSchemaName(user.ID),
				ArchiveDir:  archiveJoin("orgs", orgSlug, "personal", user.ID),
				Scope:       backup.Scope{Kind: "personal", OrgSlug: orgSlug, UserID: user.ID},
				ScopeName:   "personal:" + orgSlug + ":" + user.ID,
				Description: "personal " + orgSlug + "/" + user.ID,
			})
		}
	}
	return dbs, nil
}

func (s *Store) exportPostgresDBRows(ctx context.Context, writer *backup.Writer, manifest *backup.Manifest, dbInfo postgresLogicalDB, opts PlatformBackupExportOptions) error {
	db := s.platformDB
	closeDB := false
	if dbInfo.DBName != "" {
		dsn, err := s.deriveDSN(dbInfo.DBName)
		if err != nil {
			return err
		}
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			return fmt.Errorf("open %s: %w", dbInfo.Description, err)
		}
		db.SetMaxOpenConns(s.maxOpenConns)
		db.SetMaxIdleConns(s.maxIdleConns)
		db.SetConnMaxLifetime(s.connMaxLifetime)
		closeDB = true
	}
	if closeDB {
		defer db.Close()
	}
	if ok, err := postgresSchemaExists(ctx, db, dbInfo.Schema); err != nil {
		return fmt.Errorf("check %s schema: %w", dbInfo.Description, err)
	} else if !ok {
		return nil
	}
	return exportLogicalDBRows(ctx, writer, manifest, logicalDB{
		DB:          db,
		Dialect:     DialectPostgres,
		Schema:      dbInfo.Schema,
		ArchiveDir:  filepath.ToSlash(dbInfo.ArchiveDir),
		Scope:       dbInfo.Scope,
		ScopeName:   dbInfo.ScopeName,
		Description: dbInfo.Description,
	}, opts)
}

func postgresSchemaExists(ctx context.Context, db *sql.DB, schema string) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`, schema).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
