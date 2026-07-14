package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Layer 2: read_file line-range + line numbers ---

func TestReadFile_LineNumbers(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	content := "line one\nline two\nline three\n"
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte(content), 0644)

	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	expected := "1: line one\n2: line two\n3: line three"
	if result.Content != expected {
		t.Errorf("content = %q, want %q", result.Content, expected)
	}
	if result.TotalLines != 3 {
		t.Errorf("total_lines = %d, want 3", result.TotalLines)
	}
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, "content of line "+strings.Repeat("x", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	path := filepath.Join(dir, "big.txt")
	os.WriteFile(path, []byte(content), 0644)

	offset := 5
	limit := 3
	result, err := ReadFile(nil, ReadFileArgs{Path: path, Offset: &offset, Limit: &limit})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Should return lines 5, 6, 7 with line numbers
	if result.TotalLines != 20 {
		t.Errorf("total_lines = %d, want 20", result.TotalLines)
	}
	if result.Range != "lines 5-7 of 20" {
		t.Errorf("range = %q, want %q", result.Range, "lines 5-7 of 20")
	}

	resultLines := strings.Split(result.Content, "\n")
	if len(resultLines) != 3 {
		t.Fatalf("got %d lines, want 3", len(resultLines))
	}
	if !strings.HasPrefix(resultLines[0], "5: ") {
		t.Errorf("first line = %q, want prefix '5: '", resultLines[0])
	}
	if !strings.HasPrefix(resultLines[2], "7: ") {
		t.Errorf("third line = %q, want prefix '7: '", resultLines[2])
	}
}

func TestReadFile_OffsetPastEnd(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	os.WriteFile(path, []byte("only one line"), 0644)

	offset := 100
	result, err := ReadFile(nil, ReadFileArgs{Path: path, Offset: &offset})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if result.Content != "" {
		t.Errorf("content should be empty for offset past end, got %q", result.Content)
	}
	if result.TotalLines != 1 {
		t.Errorf("total_lines = %d, want 1", result.TotalLines)
	}
}

func TestReadFile_EmptyFile(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0644)

	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if result.TotalLines != 0 {
		t.Errorf("total_lines = %d, want 0", result.TotalLines)
	}
}

func TestReadFile_LimitExceedsTotalLines(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("a\nb\nc"), 0644)

	limit := 1000
	result, err := ReadFile(nil, ReadFileArgs{Path: path, Limit: &limit})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	// Should return all 3 lines
	if result.TotalLines != 3 {
		t.Errorf("total_lines = %d, want 3", result.TotalLines)
	}
	resultLines := strings.Split(result.Content, "\n")
	if len(resultLines) != 3 {
		t.Errorf("got %d lines, want 3", len(resultLines))
	}
}

// --- Layer 1: edit_file verification context ---

func TestEditFile_VerificationContext(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, "line_"+strings.Repeat("a", i)+"_end")
	}
	content := strings.Join(lines, "\n") + "\n"
	path := writeTestFile(t, dir, "test.txt", content)

	// Target line 15 specifically: "line_aaaaaaaaaaaaaaa_end" (15 a's)
	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "line_aaaaaaaaaaaaaaa_end",
		NewString: "REPLACED_LINE_FIFTEEN",
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if !result.Success {
		t.Fatal("Success = false, want true")
	}
	if result.VerificationContext == "" {
		t.Fatal("VerificationContext is empty, expected surrounding lines")
	}
	// Should contain the replaced text
	if !strings.Contains(result.VerificationContext, "REPLACED_LINE_FIFTEEN") {
		t.Errorf("verification_context should contain new text, got: %s", result.VerificationContext)
	}
	// Should have line numbers
	if !strings.Contains(result.VerificationContext, "15: ") {
		t.Errorf("verification_context should have line numbers, got: %s", result.VerificationContext)
	}
}

func TestEditFile_VerificationContextDeletion(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	content := "keep this\nremove this\nkeep this too\n"
	path := writeTestFile(t, dir, "test.txt", content)

	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "remove this\n",
		NewString: "", // deletion
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if !result.Success {
		t.Fatal("Success = false")
	}
	// Verification context should still be present (showing surrounding area)
	if result.VerificationContext == "" {
		t.Fatal("VerificationContext is empty for deletion")
	}
}

// --- Layer 3: File read cache ---

func TestFileReadCache_Basic(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "cached.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	// First read: should NOT be a cache hit
	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if result.Unchanged {
		t.Error("first read should not be unchanged")
	}
	if result.Content != "1: hello world" {
		t.Errorf("content = %q", result.Content)
	}

	// Second read (same path, same range): should be a cache hit
	result, err = ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if !result.Unchanged {
		t.Error("second read should be unchanged (cache hit)")
	}
	if result.TotalLines != 1 {
		t.Errorf("total_lines = %d, want 1", result.TotalLines)
	}
}

func TestFileReadCache_InvalidatedByEdit(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "edited.txt")
	os.WriteFile(path, []byte("original content"), 0644)

	// First read: populates cache
	_, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	// Edit the file (invalidates cache)
	_, err = EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "original",
		NewString: "modified",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	// Third read: should NOT be a cache hit (edit invalidated it)
	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("third read: %v", err)
	}
	if result.Unchanged {
		t.Error("read after edit should not be unchanged")
	}
	if !strings.Contains(result.Content, "modified") {
		t.Errorf("should contain 'modified', got: %s", result.Content)
	}
}

func TestFileReadCache_InvalidatedByShellCommand(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "shell.txt")
	os.WriteFile(path, []byte("before shell"), 0644)

	// First read: populates cache
	_, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	// Shell command: marks all unverified
	_, err = ShellCommand(nil, ShellCommandArgs{Command: "echo hello"})
	if err != nil {
		t.Fatalf("shell_command: %v", err)
	}

	// Modify the file externally (simulating what a shell command might do)
	os.WriteFile(path, []byte("after shell"), 0644)

	// Wait a moment to ensure mtime changes
	time.Sleep(10 * time.Millisecond)

	// Read again: since file was modified (mtime changed), should NOT be cache hit
	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("read after shell: %v", err)
	}
	if result.Unchanged {
		t.Error("read after shell+modification should not be unchanged")
	}
	if !strings.Contains(result.Content, "after shell") {
		t.Errorf("should contain 'after shell', got: %s", result.Content)
	}
}

func TestFileReadCache_ForceBypass(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "force.txt")
	os.WriteFile(path, []byte("force content"), 0644)

	// First read: populates cache
	_, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	// Second read with force=true: should return content even though unchanged
	result, err := ReadFile(nil, ReadFileArgs{Path: path, Force: true})
	if err != nil {
		t.Fatalf("force read: %v", err)
	}
	if result.Unchanged {
		t.Error("force read should never be unchanged")
	}
	if !strings.Contains(result.Content, "force content") {
		t.Errorf("force read should have content, got: %s", result.Content)
	}
}

func TestFileReadCache_MustReadBeforeEdit(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "guard.txt")
	os.WriteFile(path, []byte("guard content"), 0644)

	// Read a different file first (to activate the guard)
	otherPath := filepath.Join(dir, "other.txt")
	os.WriteFile(otherPath, []byte("other"), 0644)
	_, err := ReadFile(nil, ReadFileArgs{Path: otherPath})
	if err != nil {
		t.Fatalf("read other: %v", err)
	}

	// Now try to edit guard.txt WITHOUT reading it first
	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "guard",
		NewString: "edited",
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	// Should be rejected (not an error, but success=false)
	if result.Success {
		t.Fatal("edit without prior read should be rejected (success=false)")
	}
	if !strings.Contains(result.Message, "must read") {
		t.Errorf("expected 'must read' message, got: %s", result.Message)
	}

	// Now read the file, then edit should work
	_, err = ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("read guard.txt: %v", err)
	}

	result, err = EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "guard",
		NewString: "edited",
	})
	if err != nil {
		t.Fatalf("EditFile() after read error = %v", err)
	}
	if !result.Success {
		t.Fatalf("edit after read should succeed, got: %s", result.Message)
	}
}

func TestFileReadCache_DiskPersistence(t *testing.T) {
	// Use a dedicated cache file for this test
	dir := t.TempDir()
	cacheFilePath = filepath.Join(dir, "persist_cache.json")
	t.Cleanup(func() { cacheFilePath = defaultCacheFilePath })

	testFile := filepath.Join(dir, "persist.txt")
	os.WriteFile(testFile, []byte("persistent"), 0644)

	// First read: populates cache and writes to disk
	_, err := ReadFile(nil, ReadFileArgs{Path: testFile})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	// Verify cache file was written
	if _, err := os.Stat(cacheFilePath); os.IsNotExist(err) {
		t.Fatal("cache file was not written to disk")
	}

	// Simulate fresh process: load cache from disk directly
	cache := LoadFileReadCache()
	if cache == nil {
		t.Fatal("LoadFileReadCache returned nil")
	}
	key := buildCacheKey(testFile, 1, 0)
	entry, ok := cache.Get(key)
	if !ok {
		t.Fatal("cache entry not found after disk reload")
	}
	if entry.Source != "read" {
		t.Errorf("source = %q, want 'read'", entry.Source)
	}
	if entry.TotalLines != 1 {
		t.Errorf("total_lines = %d, want 1", entry.TotalLines)
	}
}

// --- Cross-agent cache scoping ---

func TestFileReadCache_CrossAgentScoping(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.txt")
	os.WriteFile(path, []byte("shared content"), 0644)

	// Agent "po" reads the file
	currentCaller = "po"
	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("po read: %v", err)
	}
	if result.Unchanged {
		t.Error("po first read should not be unchanged")
	}
	if result.Content != "1: shared content" {
		t.Errorf("po content = %q", result.Content)
	}

	// Agent "dev" reads the SAME file — should NOT get unchanged
	// (dev has never seen this file in its own conversation)
	currentCaller = "dev"
	result, err = ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("dev read: %v", err)
	}
	if result.Unchanged {
		t.Error("dev first read should NOT be unchanged (different caller)")
	}
	if result.Content != "1: shared content" {
		t.Errorf("dev content = %q", result.Content)
	}

	// Agent "dev" reads AGAIN — now it SHOULD get unchanged
	result, err = ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("dev second read: %v", err)
	}
	if !result.Unchanged {
		t.Error("dev second read SHOULD be unchanged (same caller, same file)")
	}

	// Agent "po" reads again — should also get unchanged (po already read it)
	currentCaller = "po"
	result, err = ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("po second read: %v", err)
	}
	if !result.Unchanged {
		t.Error("po second read SHOULD be unchanged")
	}
}

func TestFileReadCache_CrossAgentInvalidation(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "edited.txt")
	os.WriteFile(path, []byte("original"), 0644)

	// Both agents read the file
	currentCaller = "po"
	_, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("po read: %v", err)
	}
	currentCaller = "dev"
	_, err = ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("dev read: %v", err)
	}

	// Dev edits the file — should invalidate for ALL callers
	_, err = EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "original",
		NewString: "modified",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	// PO reads again — should get fresh content (edit invalidated all entries)
	currentCaller = "po"
	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("po read after edit: %v", err)
	}
	if result.Unchanged {
		t.Error("po read after edit should NOT be unchanged")
	}
	if !strings.Contains(result.Content, "modified") {
		t.Errorf("should contain 'modified', got: %s", result.Content)
	}
}

func TestFileReadCache_EmptyCallerFallback(t *testing.T) {
	resetTestCache(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "nocaller.txt")
	os.WriteFile(path, []byte("hello"), 0644)

	// Read with empty caller (single-agent mode / backwards compat)
	currentCaller = ""
	result, err := ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if result.Unchanged {
		t.Error("first read should not be unchanged")
	}

	// Second read with same empty caller — should dedup
	result, err = ReadFile(nil, ReadFileArgs{Path: path})
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if !result.Unchanged {
		t.Error("second read with same (empty) caller should be unchanged")
	}
}
