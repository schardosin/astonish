package pgstore

import (
	"net/url"
	"testing"
)

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		user     string
		password string
		dbname   string
		sslmode  string
		wantHost string // expected host:port in the URL
		wantDB   string // expected database path
		wantUser string // expected userinfo
		wantSSL  string // expected sslmode query param
	}{
		{
			name:     "all fields",
			host:     "db.example.com",
			port:     5432,
			user:     "admin",
			password: "s3cret",
			dbname:   "astonish_platform",
			sslmode:  "require",
			wantHost: "db.example.com:5432",
			wantDB:   "/astonish_platform",
			wantUser: "admin",
			wantSSL:  "require",
		},
		{
			name:     "localhost defaults",
			host:     "localhost",
			port:     5432,
			user:     "postgres",
			password: "pass",
			dbname:   "mydb",
			sslmode:  "disable",
			wantHost: "localhost:5432",
			wantDB:   "/mydb",
			wantUser: "postgres",
			wantSSL:  "disable",
		},
		{
			name:     "no password",
			host:     "localhost",
			port:     5432,
			user:     "pguser",
			password: "",
			dbname:   "testdb",
			sslmode:  "prefer",
			wantHost: "localhost:5432",
			wantDB:   "/testdb",
			wantUser: "pguser",
			wantSSL:  "prefer",
		},
		{
			name:     "no ssl mode",
			host:     "localhost",
			port:     5433,
			user:     "admin",
			password: "pass",
			dbname:   "astonish_platform",
			sslmode:  "",
			wantHost: "localhost:5433",
			wantDB:   "/astonish_platform",
			wantUser: "admin",
			wantSSL:  "",
		},
		{
			name:     "special characters in password",
			host:     "localhost",
			port:     5432,
			user:     "admin",
			password: "p@ss:w0rd/special?&=",
			dbname:   "db",
			sslmode:  "require",
			wantHost: "localhost:5432",
			wantDB:   "/db",
			wantUser: "admin",
			wantSSL:  "require",
		},
		{
			name:     "custom port",
			host:     "10.0.0.5",
			port:     15432,
			user:     "app",
			password: "secret",
			dbname:   "astonish_platform",
			sslmode:  "require",
			wantHost: "10.0.0.5:15432",
			wantDB:   "/astonish_platform",
			wantUser: "app",
			wantSSL:  "require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := BuildDSN(tt.host, tt.port, tt.user, tt.password, tt.dbname, tt.sslmode)

			// Parse the DSN as a URL to validate structure
			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("BuildDSN() produced unparseable URL: %v", err)
			}

			if u.Scheme != "postgres" {
				t.Errorf("scheme = %q, want %q", u.Scheme, "postgres")
			}
			if u.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", u.Host, tt.wantHost)
			}
			if u.Path != tt.wantDB {
				t.Errorf("path = %q, want %q", u.Path, tt.wantDB)
			}
			if u.User.Username() != tt.wantUser {
				t.Errorf("user = %q, want %q", u.User.Username(), tt.wantUser)
			}

			// Check password presence
			gotPass, hasPass := u.User.Password()
			if tt.password != "" {
				if !hasPass {
					t.Error("expected password in DSN, got none")
				} else if gotPass != tt.password {
					t.Errorf("password = %q, want %q", gotPass, tt.password)
				}
			} else {
				if hasPass {
					t.Errorf("expected no password, got %q", gotPass)
				}
			}

			// Check SSL mode
			gotSSL := u.Query().Get("sslmode")
			if gotSSL != tt.wantSSL {
				t.Errorf("sslmode = %q, want %q", gotSSL, tt.wantSSL)
			}
		})
	}
}

func TestBuildDSN_RoundTrip(t *testing.T) {
	// Verify the DSN can be round-tripped through url.Parse
	dsn := BuildDSN("myhost.com", 5432, "user", "p@ss", "mydb", "require")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}

	// Reconstruct and parse again
	dsn2 := u.String()
	u2, err := url.Parse(dsn2)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}

	if u.Host != u2.Host {
		t.Errorf("host mismatch after round-trip: %q vs %q", u.Host, u2.Host)
	}
	if u.Path != u2.Path {
		t.Errorf("path mismatch after round-trip: %q vs %q", u.Path, u2.Path)
	}
	if u.User.Username() != u2.User.Username() {
		t.Errorf("user mismatch after round-trip")
	}
}
