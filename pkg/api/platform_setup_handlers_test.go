package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store/entstore"
)

func sqliteFreshSetupRequest(t *testing.T, dataDir, mode, token string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(SQLitePlatformInitRequest{
		DataDir: dataDir, AuthMode: mode,
		AdminEmail: "fresh@example.test", AdminPassword: "SyntheticPassword123!",
		OrgName: "Fresh", OrgSlug: "fresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/platform/init/sqlite", strings.NewReader(string(payload)))
	req.Header.Set("X-Astonish-Bootstrap-Token", token)
	w := httptest.NewRecorder()
	SQLitePlatformInitHandler(w, req)
	return w
}

func sqliteFreshSetupPaths(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	configHome := filepath.Join(root, "new", "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("SUDO_USER", "")
	t.Setenv("ASTONISH_MASTER_KEY", "")
	t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "unit-bootstrap-secret")
	return filepath.Join(configHome, "astonish"), filepath.Join(root, "new", "data", "sqlite")
}

func TestSQLitePlatformInitHandler_FreshDirectories(t *testing.T) {
	for _, mode := range []string{config.AuthModeBuiltin, config.AuthModeNone} {
		t.Run(mode, func(t *testing.T) {
			configDir, dataDir := sqliteFreshSetupPaths(t)
			for _, path := range []string{configDir, dataDir} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("fresh setup requires absent config/data paths: %v", err)
				}
			}
			w := sqliteFreshSetupRequest(t, dataDir, mode, "unit-bootstrap-secret")
			if w.Code != http.StatusOK {
				t.Fatalf("fresh initialization: HTTP %d: %s", w.Code, w.Body.String())
			}
			cfg, err := config.LoadAppConfig()
			if err != nil || !cfg.Storage.IsSQLite() || cfg.Storage.SQLite.DataDir != dataDir || cfg.Storage.Auth.Mode != mode {
				t.Fatalf("fresh setup did not persist requested configuration: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dataDir, "platform.db")); err != nil {
				t.Fatal(err)
			}
			if mode == config.AuthModeBuiltin {
				key, err := os.ReadFile(filepath.Join(configDir, ".store_key"))
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := hex.DecodeString(string(key))
				if err != nil || len(decoded) != 32 {
					t.Fatal("setup did not persist a valid master key")
				}
				info, err := os.Stat(filepath.Join(configDir, ".store_key"))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("key must be owner-only: %v", err)
				}
			} else if _, err := os.Stat(filepath.Join(configDir, ".store_key")); !os.IsNotExist(err) {
				t.Fatalf("no-auth setup must not implicitly generate an encryption key: %v", err)
			}
		})
	}
}

func TestSQLitePlatformInitHandler_KeyPersistenceFailure(t *testing.T) {
	configDir, dataDir := sqliteFreshSetupPaths(t)
	// A directory at the key filename fails reliably even when tests run as root.
	keyPath := filepath.Join(configDir, ".store_key")
	if err := os.MkdirAll(keyPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	w := sqliteFreshSetupRequest(t, dataDir, config.AuthModeBuiltin, "unit-bootstrap-secret")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("key failure must reject setup, got HTTP %d: %s", w.Code, w.Body.String())
	}
	var response PlatformInitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.RestartRequired || response.Error != "Failed to prepare master encryption key" {
		t.Fatalf("expected bounded key persistence error, got %+v", response)
	}
	for _, path := range []string{filepath.Join(dataDir, "platform.db"), filepath.Join(configDir, "config.yaml")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("key failure must precede database/config persistence: %s: %v", path, err)
		}
	}
}

func TestSQLitePlatformInitHandler_KeyCreationFailure(t *testing.T) {
	configDir, dataDir := sqliteFreshSetupPaths(t)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	// A dangling symlink gives ReadFile ENOENT but must not be followed when
	// creating the key. Both link and target are isolated temporary paths.
	target := filepath.Join(configDir, "missing", "key")
	keyPath := filepath.Join(configDir, ".store_key")
	if err := os.Symlink(target, keyPath); err != nil {
		t.Fatal(err)
	}
	w := sqliteFreshSetupRequest(t, dataDir, config.AuthModeBuiltin, "unit-bootstrap-secret")
	var response PlatformInitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusInternalServerError || response.Error != "Failed to prepare master encryption key" {
		t.Fatalf("expected bounded exclusive-key-creation failure: HTTP %d: %+v", w.Code, response)
	}
	if link, err := os.Readlink(keyPath); err != nil || link != target {
		t.Fatalf("setup must preserve existing key path: %v", err)
	}
	for _, path := range []string{target, filepath.Join(dataDir, "platform.db"), filepath.Join(configDir, "config.yaml")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed key creation must not persist key/database/config: %s: %v", path, err)
		}
	}
}

func TestSQLitePlatformInitHandler_PreservesExistingKey(t *testing.T) {
	configDir, dataDir := sqliteFreshSetupPaths(t)
	for _, dir := range []string{configDir, dataDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	keyPath := filepath.Join(configDir, ".store_key")
	original := strings.Repeat("ab", 32) + "\n"
	if err := os.WriteFile(keyPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	w := sqliteFreshSetupRequest(t, dataDir, config.AuthModeBuiltin, "unit-bootstrap-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("existing-key setup: HTTP %d: %s", w.Code, w.Body.String())
	}
	key, err := os.ReadFile(keyPath)
	if err != nil || string(key) != original {
		t.Fatalf("setup must preserve the existing key byte-for-byte: %v", err)
	}
}

type sqliteSetupFaultKeyFile struct {
	*os.File
	fault      string
	failure    error
	calls      []string
	before     func(string)
	afterClose func()
}

func (f *sqliteSetupFaultKeyFile) WriteString(value string) (int, error) {
	f.calls = append(f.calls, "write")
	if f.before != nil {
		f.before("write")
	}
	if f.fault == "short-write" || f.fault == "write" {
		n, err := f.File.WriteString(value[:len(value)/2])
		if err != nil {
			return n, err
		}
		if f.fault == "write" {
			return n, f.failure
		}
		return n, nil
	}
	return f.File.WriteString(value)
}

func (f *sqliteSetupFaultKeyFile) Sync() error {
	f.calls = append(f.calls, "sync")
	if f.before != nil {
		f.before("sync")
	}
	if f.fault == "sync" {
		return f.failure
	}
	return f.File.Sync()
}

func (f *sqliteSetupFaultKeyFile) Close() error {
	f.calls = append(f.calls, "close")
	if f.before != nil {
		f.before("close")
	}
	err := f.File.Close()
	if err == nil && f.afterClose != nil {
		f.afterClose()
	}
	if f.fault == "close" && err == nil {
		return f.failure
	}
	return err
}

// Reenter at a deterministic interleaving instead of relying on scheduler timing.
// The second preparer represents another process, outside platformInitMu.
func TestPrepareSQLiteSetupMasterKey_ConcurrentAdoption(t *testing.T) {
	for _, fault := range []string{"sync", "close"} {
		t.Run(fault, func(t *testing.T) {
			configDir, _ := sqliteFreshSetupPaths(t)
			keyPath := filepath.Join(configDir, ".store_key")
			failure := errors.New("synthetic persistence failure")
			var winner []byte
			err := prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile {
				return &sqliteSetupFaultKeyFile{File: f, fault: fault, failure: failure, before: func(op string) {
					if op != fault {
						return
					}
					if _, err := os.Lstat(keyPath); !os.IsNotExist(err) {
						t.Errorf("uncommitted key visible before %s completes: %v", fault, err)
					}
					if err := prepareSQLiteSetupMasterKey(); err != nil {
						t.Fatalf("competing preparer: %v", err)
					}
					var err error
					winner, err = os.ReadFile(keyPath)
					if err != nil {
						t.Fatal(err)
					}
				}}
			})
			if !errors.Is(err, failure) {
				t.Fatalf("preparer error = %v, want injected failure", err)
			}
			got, err := os.ReadFile(keyPath)
			if err != nil || string(got) != string(winner) {
				t.Fatalf("failed writer removed/changed successfully adopted key: %v", err)
			}
		})
	}
}

func TestPrepareSQLiteSetupMasterKey_PreservesReplacementOnFailure(t *testing.T) {
	configDir, _ := sqliteFreshSetupPaths(t)
	keyPath := filepath.Join(configDir, ".store_key")
	winner := strings.Repeat("cd", 32)
	failure := errors.New("synthetic sync failure")
	err := prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile {
		return &sqliteSetupFaultKeyFile{File: f, fault: "sync", failure: failure, before: func(op string) {
			if op != "sync" {
				return
			}
			if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := os.WriteFile(keyPath, []byte(winner), 0600); err != nil {
				t.Fatal(err)
			}
		}}
	})
	if !errors.Is(err, failure) {
		t.Fatalf("preparer error = %v, want injected failure", err)
	}
	got, err := os.ReadFile(keyPath)
	if err != nil || string(got) != winner {
		t.Fatalf("failed writer removed/changed replacement key: %v", err)
	}
}

func assertSQLiteSetupTempsRemoved(t *testing.T, configDir string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(configDir, ".store_key-*"))
	if err != nil || len(paths) != 0 {
		t.Errorf("temporary key files remain: %v (error: %v)", paths, err)
	}
}

func TestPrepareSQLiteSetupMasterKey_AtomicPublication(t *testing.T) {
	configDir, _ := sqliteFreshSetupPaths(t)
	keyPath := filepath.Join(configDir, ".store_key")
	var candidateInfo os.FileInfo
	err := prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile {
		var err error
		candidateInfo, err = f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(f.Name()) != configDir || f.Name() == keyPath {
			t.Fatal("candidate must have a private same-directory pathname")
		}
		return &sqliteSetupFaultKeyFile{File: f, before: func(op string) {
			if _, err := os.Lstat(keyPath); !os.IsNotExist(err) {
				t.Errorf("key visible before %s completed: %v", op, err)
			}
		}, afterClose: func() {
			if _, err := os.Lstat(keyPath); !os.IsNotExist(err) {
				t.Errorf("key visible before close returned: %v", err)
			}
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	publishedInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(candidateInfo, publishedInfo) {
		t.Fatal("publication did not link the fully persisted candidate")
	}
	assertSQLiteSetupTempsRemoved(t, configDir)
}

func TestPrepareSQLiteSetupMasterKey_PublicationConflict(t *testing.T) {
	for _, winner := range []string{strings.Repeat("ef", 32) + "\n", "invalid", ""} {
		t.Run(strconv.Itoa(len(winner)), func(t *testing.T) {
			configDir, _ := sqliteFreshSetupPaths(t)
			keyPath := filepath.Join(configDir, ".store_key")
			var candidate *os.File
			err := prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile {
				candidate = f
				return &sqliteSetupFaultKeyFile{File: f, before: func(op string) {
					if op == "close" {
						if err := os.WriteFile(keyPath, []byte(winner), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}}
			})
			if (err == nil) != (len(winner) == 65) {
				t.Fatalf("competing key validation: error = %v", err)
			}
			got, readErr := os.ReadFile(keyPath)
			if readErr != nil || string(got) != winner {
				t.Fatalf("competing key overwritten: %v", readErr)
			}
			if candidate == nil {
				t.Fatal("publication race not exercised")
			}
			assertSQLiteSetupTempsRemoved(t, configDir)
		})
	}
}

func TestPrepareSQLiteSetupMasterKey_PublicationFailure(t *testing.T) {
	configDir, _ := sqliteFreshSetupPaths(t)
	// Remove the closed candidate to force a real Link failure, portably even
	// on systems that cannot unlink an open file. No filesystem hook is needed.
	err := prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile {
		return &sqliteSetupFaultKeyFile{File: f, afterClose: func() {
			if err := os.Remove(f.Name()); err != nil {
				t.Fatal(err)
			}
		}}
	})
	if !os.IsNotExist(err) {
		t.Fatalf("missing publication source must fail: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(configDir, ".store_key")); !os.IsNotExist(err) {
		t.Fatalf("publication failure left a final key: %v", err)
	}
	assertSQLiteSetupTempsRemoved(t, configDir)
}

func TestPrepareSQLiteSetupMasterKey_ConcurrentCreators(t *testing.T) {
	configDir, _ := sqliteFreshSetupPaths(t)
	const creators = 8
	ready := make(chan struct{}, creators)
	release := make(chan struct{})
	results := make(chan error, creators)
	for i := 0; i < creators; i++ {
		go func() {
			results <- prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile {
				ready <- struct{}{}
				<-release
				return f
			})
		}()
	}
	for i := 0; i < creators; i++ {
		<-ready
	}
	if _, err := os.Lstat(filepath.Join(configDir, ".store_key")); !os.IsNotExist(err) {
		t.Errorf("provisional key published: %v", err)
	}
	close(release)
	for i := 0; i < creators; i++ {
		if err := <-results; err != nil {
			t.Errorf("competing creator failed: %v", err)
		}
	}
	keyPath := filepath.Join(configDir, ".store_key")
	winner, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(string(winner))
	if err != nil || len(decoded) != 32 {
		t.Fatalf("published key is invalid: %v", err)
	}
	if err := prepareSQLiteSetupMasterKey(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(keyPath)
	if err != nil || string(got) != string(winner) {
		t.Fatalf("winner changed after contention: %v", err)
	}
	assertSQLiteSetupTempsRemoved(t, configDir)
}

func TestSQLitePlatformInitHandler_KeyFileOperationFailures(t *testing.T) {
	for _, fault := range []string{"short-write", "write", "sync", "close"} {
		for _, existingConfig := range []bool{false, true} {
			name := fault + "/fresh-config"
			if existingConfig {
				name = fault + "/existing-config"
			}
			t.Run(name, func(t *testing.T) {
				configDir, dataDir := sqliteFreshSetupPaths(t)
				configPath := filepath.Join(configDir, "config.yaml")
				originalConfig := []byte("storage:\n  auth:\n    jwt_secret: existing-jwt-sentinel\n")
				if existingConfig {
					if err := os.MkdirAll(configDir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(configPath, originalConfig, 0600); err != nil {
						t.Fatal(err)
					}
				}
				failure := errors.New("synthetic private key error: " + configDir)
				var file *sqliteSetupFaultKeyFile
				var keyErr error
				prepare := func() error {
					keyErr = prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile {
						file = &sqliteSetupFaultKeyFile{File: f, fault: fault, failure: failure}
						return file
					})
					return keyErr
				}
				req := httptest.NewRequest(http.MethodPost, "/api/platform/init/sqlite", strings.NewReader(`{"data_dir":`+strconv.Quote(dataDir)+`,"auth_mode":"builtin","admin_email":"fresh@example.test","admin_password":"SyntheticPassword123!","org_name":"Fresh","org_slug":"fresh"}`))
				req.Header.Set("X-Astonish-Bootstrap-Token", "unit-bootstrap-secret")
				w := httptest.NewRecorder()
				sqlitePlatformInitHandler(w, req, prepare)
				wantErr := failure
				if fault == "short-write" {
					wantErr = io.ErrShortWrite
				}
				if !errors.Is(keyErr, wantErr) {
					t.Errorf("key error = %v, want %v", keyErr, wantErr)
				}
				var response PlatformInitResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusInternalServerError || response.Success || response.RestartRequired || response.Error != "Failed to prepare master encryption key" {
					t.Errorf("expected bounded key failure without success: HTTP %d: %+v", w.Code, response)
				}
				if strings.Contains(w.Body.String(), configDir) || strings.Contains(w.Body.String(), "synthetic private") {
					t.Error("response disclosed internal key error")
				}
				if file == nil {
					t.Fatal("file seam was not exercised")
				}
				wantCalls := "write,sync,close"
				if fault == "short-write" || fault == "write" {
					wantCalls = "write,close"
				}
				if got := strings.Join(file.calls, ","); got != wantCalls {
					t.Errorf("operations = %s, want %s", got, wantCalls)
				}
				for _, path := range []string{filepath.Join(configDir, ".store_key"), filepath.Join(dataDir, "platform.db")} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Errorf("key failure must leave no key/database: %s: %v", path, err)
					}
				}
				assertSQLiteSetupTempsRemoved(t, configDir)
				if existingConfig {
					got, err := os.ReadFile(configPath)
					if err != nil || string(got) != string(originalConfig) {
						t.Errorf("key failure changed configuration/JWT: %v", err)
					}
				} else if _, err := os.Stat(configPath); !os.IsNotExist(err) {
					t.Errorf("key failure persisted configuration/JWT: %v", err)
				}
			})
		}
	}
}

func TestPrepareSQLiteSetupMasterKey_FileOpsPreserveExistingKey(t *testing.T) {
	configDir, _ := sqliteFreshSetupPaths(t)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(configDir, ".store_key")
	original := strings.Repeat("ab", 32)
	if err := os.WriteFile(keyPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareSQLiteSetupMasterKeyWithFileOps(func(*os.File) sqliteSetupKeyFile {
		t.Fatal("existing valid key must not be opened for writing")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(keyPath)
	if err != nil || string(got) != original {
		t.Fatalf("existing key changed: %v", err)
	}
}

type setupFailingRandomReader struct{}

func (setupFailingRandomReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic entropy failure")
}

func TestPrepareSQLiteSetupMasterKey_RandomFailure(t *testing.T) {
	configDir, _ := sqliteFreshSetupPaths(t)
	original := rand.Reader
	rand.Reader = setupFailingRandomReader{}
	t.Cleanup(func() { rand.Reader = original })
	if err := prepareSQLiteSetupMasterKey(); err == nil {
		t.Fatal("entropy failure must not be ignored")
	}
	if _, err := os.Stat(filepath.Join(configDir, ".store_key")); !os.IsNotExist(err) {
		t.Fatalf("entropy failure must not persist a key: %v", err)
	}
}

func TestSQLitePlatformInitHandler_MasterKeySources(t *testing.T) {
	for _, tc := range []struct {
		name, environment, file string
		wantStatus              int
	}{
		{name: "environment", environment: strings.Repeat("cd", 32), wantStatus: http.StatusOK},
		{name: "environment_precedes_file", environment: strings.Repeat("cd", 32), file: "untouched-invalid-file", wantStatus: http.StatusOK},
		{name: "invalid_environment", environment: "not-a-key", wantStatus: http.StatusInternalServerError},
		{name: "invalid_file", file: "not-a-key", wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configDir, dataDir := sqliteFreshSetupPaths(t)
			t.Setenv("ASTONISH_MASTER_KEY", tc.environment)
			keyPath := filepath.Join(configDir, ".store_key")
			if tc.file != "" {
				if err := os.MkdirAll(configDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(keyPath, []byte(tc.file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			w := sqliteFreshSetupRequest(t, dataDir, config.AuthModeBuiltin, "unit-bootstrap-secret")
			if w.Code != tc.wantStatus {
				t.Fatalf("HTTP %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			key, err := os.ReadFile(keyPath)
			if tc.file == "" {
				if !os.IsNotExist(err) {
					t.Fatalf("must not generate fallback key with environment key: %v", err)
				}
			} else if err != nil || string(key) != tc.file {
				t.Fatalf("must not overwrite existing key: %v", err)
			}
			if tc.wantStatus == http.StatusInternalServerError {
				var response PlatformInitResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error != "Failed to prepare master encryption key" {
					t.Fatalf("expected bounded key error: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dataDir, "platform.db")); !os.IsNotExist(err) {
					t.Fatalf("invalid key must stop database creation: %v", err)
				}
			}
		})
	}
}

func TestSQLitePlatformInitHandler_DataDirectoryFailure(t *testing.T) {
	configDir, dataDir := sqliteFreshSetupPaths(t)
	if err := os.MkdirAll(filepath.Dir(dataDir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	w := sqliteFreshSetupRequest(t, dataDir, config.AuthModeBuiltin, "unit-bootstrap-secret")
	var response PlatformInitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusInternalServerError || response.Error != "Failed to create SQLite data directory" {
		t.Fatalf("expected bounded directory failure: HTTP %d: %+v", w.Code, response)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("data directory failure must not persist config/key: %v", err)
	}
}

func TestSQLitePlatformInitHandler_FreshDirectoriesUnauthorized(t *testing.T) {
	configDir, dataDir := sqliteFreshSetupPaths(t)
	w := sqliteFreshSetupRequest(t, dataDir, config.AuthModeBuiltin, "wrong-token")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got HTTP %d", w.Code)
	}
	for _, path := range []string{configDir, dataDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unauthorized setup created a directory: %s: %v", path, err)
		}
	}
}

func TestSQLitePlatformInitHandler_BuiltinMemberships(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("SUDO_USER", "")
	t.Setenv("ASTONISH_MASTER_KEY", "")
	t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "unit-bootstrap-secret")
	dataDir := filepath.Join(root, "data")
	for _, dir := range []string{dataDir, filepath.Join(root, "astonish")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	payload, err := json.Marshal(SQLitePlatformInitRequest{
		DataDir: dataDir, AuthMode: config.AuthModeBuiltin,
		AdminEmail: "bootstrap@example.test", AdminPassword: "SyntheticPassword123!",
		OrgName: "Bootstrap", OrgSlug: "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/platform/init/sqlite", strings.NewReader(string(payload)))
	req.Header.Set("X-Astonish-Bootstrap-Token", "unit-bootstrap-secret")
	w := httptest.NewRecorder()
	SQLitePlatformInitHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("builtin initialization: HTTP %d: %s", w.Code, w.Body.String())
	}
	ctx := context.Background()
	es, err := entstore.New(ctx, entstore.Config{DSN: "file:" + filepath.Join(dataDir, "platform.db"), DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	admin, err := es.Users().GetByEmail(ctx, "bootstrap@example.test")
	if err != nil {
		t.Fatal(err)
	}
	org, err := es.Organizations().GetBySlug(ctx, "bootstrap")
	if err != nil || org == nil || org.Status != "active" {
		t.Fatalf("expected active bootstrap organization: %v", err)
	}
	role, err := es.Organizations().GetMemberRole(ctx, admin.ID, org.ID)
	if err != nil || role != "owner" {
		t.Fatalf("expected org owner, got %q: %v", role, err)
	}
	ods, err := es.ForOrg(org.Slug)
	if err != nil {
		t.Fatal(err)
	}
	teams, err := ods.Teams().ListTeams(ctx)
	if err != nil || len(teams) != 1 || teams[0].Slug != "general" {
		t.Fatalf("expected default General team: %v", err)
	}
	role, err = ods.Teams().GetMemberRole(ctx, admin.ID, teams[0].ID)
	if err != nil || role != "admin" {
		t.Fatalf("expected bootstrap General team admin membership, got %q: %v", role, err)
	}
}

// --- cleanPGError Tests ---

func TestCleanPGError_RemovesConnectionString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "connection string at end",
			input: "failed to connect: postgres://user:pass@host:5432/db",
			want:  "failed to connect: [redacted]",
		},
		{
			name:  "connection string in middle",
			input: "error connecting to postgres://admin:secret@db.example.com:5432/astonish_platform more text",
			want:  "error connecting to [redacted] more text",
		},
		{
			name:  "no connection string",
			input: "some ordinary error message",
			want:  "some ordinary error message",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "connection string with special chars",
			input: "failed: postgres://u:p%40ss@host:5432/db?sslmode=require",
			want:  "failed: [redacted]",
		},
		{
			name:  "connection string with tab after",
			input: "err postgres://x:y@h:5432/d\tnext",
			want:  "err [redacted]\tnext",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanPGError(tt.input)
			if got != tt.want {
				t.Errorf("cleanPGError(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- DeploymentModeHandler Tests ---

// setupTestConfigDir creates a temp XDG_CONFIG_HOME with an optional config.yaml
// and returns a cleanup function.
func setupTestConfigDir(t *testing.T, cfg *config.AppConfig) func() {
	t.Helper()
	tmpDir := t.TempDir()
	astonishDir := filepath.Join(tmpDir, "astonish")
	if err := os.MkdirAll(astonishDir, 0755); err != nil {
		t.Fatalf("failed to create test config dir: %v", err)
	}

	origXDG := os.Getenv("XDG_CONFIG_HOME")

	os.Setenv("XDG_CONFIG_HOME", tmpDir)

	if cfg != nil {
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(astonishDir, "config.yaml"), data, 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}
	}

	return func() {
		if origXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}
}

func TestDeploymentModeHandler_PersonalMode(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/platform/mode", nil)
	w := httptest.NewRecorder()
	DeploymentModeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode error: %v", err)
	}

	if resp["mode"] != "personal" {
		t.Errorf("mode = %q, want %q", resp["mode"], "personal")
	}
}

func TestDeploymentModeHandler_PlatformMode(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{
			Backend: "postgres",
			Postgres: config.PostgresConfig{
				PlatformDSN: "postgres://u:p@h:5432/db",
			},
		},
	})
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/platform/mode", nil)
	w := httptest.NewRecorder()
	DeploymentModeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode error: %v", err)
	}

	if resp["mode"] != "platform" {
		t.Errorf("mode = %q, want %q", resp["mode"], "platform")
	}
	if resp["configured"] != true {
		t.Errorf("configured = %v, want true", resp["configured"])
	}
}

func TestDeploymentModeHandler_NoConfigFile(t *testing.T) {
	// No config file written — should default to personal mode
	cleanup := setupTestConfigDir(t, nil)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/platform/mode", nil)
	w := httptest.NewRecorder()
	DeploymentModeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["mode"] != "personal" {
		t.Errorf("mode = %q, want %q", resp["mode"], "personal")
	}
}

// --- PlatformInitHandler Tests ---

// authorizedPlatformInitTestHandler supplies the explicit setup capability and
// cancels network work: validation tests must never contact a real PG server.
func authorizedPlatformInitTestHandler(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "unit-bootstrap-secret")
	r.Header.Set("X-Astonish-Bootstrap-Token", "unit-bootstrap-secret")
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	PlatformInitHandler(w, r.WithContext(ctx))
}

func TestPlatformInitHandler_AlreadyConfigured(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{
			Backend: "postgres",
			Postgres: config.PostgresConfig{
				PlatformDSN: "postgres://u:p@h:5432/db",
			},
		},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"admin","password":"pass"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	authorizedPlatformInitTestHandler(t, w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "already configured") {
		t.Errorf("error = %q, want to contain 'already configured'", resp.Error)
	}
}

func TestPlatformInitHandler_MissingUser(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"","password":"pass"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	authorizedPlatformInitTestHandler(t, w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "username is required") {
		t.Errorf("error = %q, want to contain 'username is required'", resp.Error)
	}
}

func TestPlatformInitHandler_MissingPassword(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"admin","password":""}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	authorizedPlatformInitTestHandler(t, w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "password is required") {
		t.Errorf("error = %q, want to contain 'password is required'", resp.Error)
	}
}

func TestPlatformInitHandler_InvalidSlug(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"admin","password":"pass","org_slug":"INVALID SLUG!"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	authorizedPlatformInitTestHandler(t, w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "lowercase alphanumeric") {
		t.Errorf("error = %q, want to contain 'lowercase alphanumeric'", resp.Error)
	}
}

func TestPlatformInitHandler_InvalidJSON(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{invalid json`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	authorizedPlatformInitTestHandler(t, w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "Invalid request body") {
		t.Errorf("error = %q, want to contain 'Invalid request body'", resp.Error)
	}
}

func TestPlatformInitHandler_DefaultsApplied(t *testing.T) {
	// This test validates that default values are applied for optional fields.
	// It won't succeed at actually connecting (no real PG), but we can test
	// that it gets past validation. We expect a 500 because PG isn't available.
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	// Minimal valid request — host/port/ssl/org all omitted (will use defaults)
	body := `{"user":"admin","password":"pass"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	authorizedPlatformInitTestHandler(t, w, req)

	// Should not be 400 (validation should pass with defaults applied)
	// It will be 500 because there's no real PostgreSQL to connect to
	if w.Code == http.StatusBadRequest {
		var resp PlatformInitResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		t.Fatalf("unexpected 400: %s (defaults should handle missing fields)", resp.Error)
	}

	// Accept 500 (can't connect to PG) — the key is that validation passed
	if w.Code != http.StatusInternalServerError {
		t.Logf("status = %d (expected 500 since no PG is available)", w.Code)
	}
}

func TestPlatformInitHandler_ValidSlugCharacters(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	validSlugs := []string{"my-org", "org_1", "test123", "a", "org-name-with-hyphens"}

	for _, slug := range validSlugs {
		t.Run(slug, func(t *testing.T) {
			body := `{"user":"admin","password":"pass","org_slug":"` + slug + `"}`
			req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			authorizedPlatformInitTestHandler(t, w, req)

			// Should not be rejected with 400 for slug validation
			if w.Code == http.StatusBadRequest {
				var resp PlatformInitResponse
				json.Unmarshal(w.Body.Bytes(), &resp)
				if strings.Contains(resp.Error, "slug") {
					t.Errorf("valid slug %q was rejected: %s", slug, resp.Error)
				}
			}
		})
	}
}

func TestPlatformInitHandler_InvalidSlugCharacters(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	invalidSlugs := []string{"UPPERCASE", "has space", "special!", "with.dots", "slash/bad"}

	for _, slug := range invalidSlugs {
		t.Run(slug, func(t *testing.T) {
			body := `{"user":"admin","password":"pass","org_slug":"` + slug + `"}`
			req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			authorizedPlatformInitTestHandler(t, w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("invalid slug %q was NOT rejected (status=%d)", slug, w.Code)
			}
		})
	}
}
