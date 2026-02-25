package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPDF_Validation(t *testing.T) {
	tests := []struct {
		name    string
		args    ReadPDFArgs
		wantErr string
	}{
		{
			name:    "EmptyPath",
			args:    ReadPDFArgs{Path: ""},
			wantErr: "path is required",
		},
		{
			name:    "NonexistentFile",
			args:    ReadPDFArgs{Path: "/tmp/nonexistent-astonish-test.pdf"},
			wantErr: "file not found",
		},
		{
			name:    "DirectoryPath",
			args:    ReadPDFArgs{Path: os.TempDir()},
			wantErr: "directory",
		},
		{
			name:    "UnsupportedScheme",
			args:    ReadPDFArgs{Path: "ftp://example.com/file.pdf"},
			wantErr: "only http and https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadPDF(nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !containsSubstring(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestReadPDF_NonPDFExtension(t *testing.T) {
	// Create a temp file with non-pdf extension
	tmp, err := os.CreateTemp("", "astonish-test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString("not a pdf")
	tmp.Close()

	_, err = ReadPDF(nil, ReadPDFArgs{Path: tmp.Name()})
	if err == nil {
		t.Fatal("expected error for non-PDF file, got nil")
	}
	if !containsSubstring(err.Error(), ".pdf extension") {
		t.Errorf("error = %q, want to mention .pdf extension", err.Error())
	}
}

func TestReadPDF_InvalidPDFContent(t *testing.T) {
	// Create a temp file with .pdf extension but invalid content
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.pdf")
	if err := os.WriteFile(path, []byte("this is not a valid PDF file"), 0644); err != nil {
		t.Fatalf("failed to create fake PDF: %v", err)
	}

	_, err := ReadPDF(nil, ReadPDFArgs{Path: path})
	if err == nil {
		t.Fatal("expected error for invalid PDF, got nil")
	}
	if !containsSubstring(err.Error(), "failed to open PDF") {
		t.Errorf("error = %q, want to mention 'failed to open PDF'", err.Error())
	}
}

func TestResolvePDFPath_LocalFile(t *testing.T) {
	// Create a valid temp file with .pdf extension
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 fake"), 0644); err != nil {
		t.Fatalf("failed to create test PDF: %v", err)
	}

	localPath, tempFile, err := resolvePDFPath(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if localPath != path {
		t.Errorf("localPath = %q, want %q", localPath, path)
	}
	if tempFile != "" {
		t.Errorf("tempFile should be empty for local files, got %q", tempFile)
	}
}

func TestResolvePDFPath_URLDetection(t *testing.T) {
	// HTTP URL should trigger download (which will fail for invalid URL, but we can test detection)
	_, _, err := resolvePDFPath("http://this-host-does-not-exist-astonish.invalid/test.pdf")
	if err == nil {
		t.Fatal("expected error for unreachable URL, got nil")
	}
	// It should have attempted a download, not a file stat
	if containsSubstring(err.Error(), "file not found") {
		t.Error("should detect URL, not treat as local file")
	}
}

func TestResolvePDFPath_SSRFBlock(t *testing.T) {
	_, _, err := resolvePDFPath("http://127.0.0.1/secret.pdf")
	if err == nil {
		t.Fatal("expected SSRF block, got nil")
	}
	if !containsSubstring(err.Error(), "private/loopback") {
		t.Errorf("error = %q, want SSRF block message", err.Error())
	}
}

func TestParseAndValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "ValidHTTPS", url: "https://example.com/file.pdf", wantErr: false},
		{name: "ValidHTTP", url: "http://example.com/file.pdf", wantErr: false},
		{name: "FTP", url: "ftp://example.com/file.pdf", wantErr: true},
		{name: "Invalid", url: "not a url", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAndValidateURL(tt.url)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
