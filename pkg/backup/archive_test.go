package backup

import (
	"archive/tar"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveCreateInspectVerify(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "test.astonish-backup")
	writer, err := Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := writer.AddFile("platform/users.jsonl", strings.NewReader("{\"id\":\"u1\"}\n")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Entries = []Entry{{
		Path:    "platform/users.jsonl",
		Kind:    "jsonl",
		Scope:   Scope{Kind: "platform"},
		Entity:  "users",
		Records: 1,
	}}
	if err := writer.CloseWithManifest(manifest); err != nil {
		t.Fatalf("CloseWithManifest() error = %v", err)
	}

	summary, err := Inspect(archivePath)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if summary.Manifest.Backend != "sqlite" {
		t.Fatalf("Inspect() backend = %q, want sqlite", summary.Manifest.Backend)
	}
	if summary.Manifest.Compression != string(CompressionGzip) {
		t.Fatalf("Inspect() compression = %q, want gzip", summary.Manifest.Compression)
	}
	if len(summary.Checksums) != 1 {
		t.Fatalf("Inspect() checksums = %d, want 1", len(summary.Checksums))
	}

	if _, err := Verify(archivePath); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestArchiveCreateDefaultsToGzip(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "test.astonish-backup")
	writer, err := Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := writer.AddFile("platform/users.jsonl", strings.NewReader("{}\n")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Entries = []Entry{{Path: "platform/users.jsonl", Kind: "jsonl", Scope: Scope{Kind: "platform"}, Entity: "users", Records: 1}}
	if err := writer.CloseWithManifest(manifest); err != nil {
		t.Fatalf("CloseWithManifest() error = %v", err)
	}

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Fatalf("archive does not start with gzip magic bytes: %x", data[:2])
	}

	files, err := ReadArchiveFiles(archivePath)
	if err != nil {
		t.Fatalf("ReadArchiveFiles() error = %v", err)
	}
	if got := string(files["platform/users.jsonl"]); got != "{}\n" {
		t.Fatalf("payload = %q, want {} newline", got)
	}
}

func TestArchiveCreateSupportsUncompressedTar(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "test.astonish-backup")
	writer, err := Create(archivePath, WriterOptions{Compression: CompressionNone})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := writer.AddFile("platform/users.jsonl", strings.NewReader("{}\n")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Entries = []Entry{{Path: "platform/users.jsonl", Kind: "jsonl", Scope: Scope{Kind: "platform"}, Entity: "users", Records: 1}}
	if err := writer.CloseWithManifest(manifest); err != nil {
		t.Fatalf("CloseWithManifest() error = %v", err)
	}

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		t.Fatal("archive is gzip-compressed, want uncompressed tar")
	}
	if _, err := Verify(archivePath); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestEncryptedArchiveRequiresPassphrase(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "plain.astonish-backup")
	writer, err := Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := writer.AddFile("platform/users.jsonl", strings.NewReader("{}\n")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Entries = []Entry{{Path: "platform/users.jsonl", Kind: "jsonl", Scope: Scope{Kind: "platform"}, Entity: "users", Records: 1}}
	if err := writer.CloseWithManifest(manifest); err != nil {
		t.Fatalf("CloseWithManifest() error = %v", err)
	}
	encryptedPath := filepath.Join(t.TempDir(), "encrypted.astonish-backup")
	if err := EncryptArchiveFile(archivePath, encryptedPath, "secret"); err != nil {
		t.Fatalf("EncryptArchiveFile() error = %v", err)
	}
	if _, err := Verify(encryptedPath); err == nil {
		t.Fatal("Verify() error = nil, want passphrase required")
	}
	if _, err := Verify(encryptedPath, ReaderOptions{Passphrase: "wrong"}); err == nil {
		t.Fatal("Verify() error = nil, want wrong passphrase error")
	}
	if _, err := Verify(encryptedPath, ReaderOptions{Passphrase: "secret"}); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestEncryptedArchiveMetadataIncludesKDFParameters(t *testing.T) {
	archivePath := createSmallArchive(t)
	encryptedPath := filepath.Join(t.TempDir(), "encrypted.astonish-backup")
	if err := EncryptArchiveFile(archivePath, encryptedPath, "secret"); err != nil {
		t.Fatalf("EncryptArchiveFile() error = %v", err)
	}
	metadata, _ := readEncryptedArchiveParts(t, encryptedPath)
	var info EncryptionInfo
	if err := json.Unmarshal(metadata, &info); err != nil {
		t.Fatalf("Unmarshal metadata error = %v", err)
	}
	if info.Version != 1 {
		t.Fatalf("Version = %d, want 1", info.Version)
	}
	if info.KDF != "argon2id" {
		t.Fatalf("KDF = %q, want argon2id", info.KDF)
	}
	if info.Argon2id != defaultArgon2idParameters {
		t.Fatalf("Argon2id = %+v, want %+v", info.Argon2id, defaultArgon2idParameters)
	}
}

func TestDecryptArchiveSupportsLegacyKDFMetadata(t *testing.T) {
	archivePath := createSmallArchive(t)
	plaintext, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	salt, err := hex.DecodeString("00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatalf("Decode salt error = %v", err)
	}
	nonce, err := hex.DecodeString("00112233445566778899aabb")
	if err != nil {
		t.Fatalf("Decode nonce error = %v", err)
	}
	metadata := []byte(`{"cipher":"AES-256-GCM","kdf":"argon2id:m=65536,t=1,p=4","salt":"00112233445566778899aabbccddeeff","nonce":"00112233445566778899aabb"}`)
	key := deriveArchiveKey("secret", salt, defaultArgon2idParameters)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("newGCM() error = %v", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, metadata)
	var out bytes.Buffer
	out.WriteString(encryptedArchiveMagic)
	out.Write(metadata)
	out.WriteByte('\n')
	out.Write(ciphertext)
	encryptedPath := filepath.Join(t.TempDir(), "legacy-encrypted.astonish-backup")
	if err := os.WriteFile(encryptedPath, out.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Verify(encryptedPath, ReaderOptions{Passphrase: "secret"}); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestEncryptedArchiveTamperDetection(t *testing.T) {
	archivePath := createSmallArchive(t)
	encryptedPath := filepath.Join(t.TempDir(), "encrypted.astonish-backup")
	if err := EncryptArchiveFile(archivePath, encryptedPath, "secret"); err != nil {
		t.Fatalf("EncryptArchiveFile() error = %v", err)
	}
	data, err := os.ReadFile(encryptedPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	data[len(data)-1] ^= 0xff
	tamperedPath := filepath.Join(t.TempDir(), "tampered.astonish-backup")
	if err := os.WriteFile(tamperedPath, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Verify(tamperedPath, ReaderOptions{Passphrase: "secret"}); err == nil {
		t.Fatal("Verify() error = nil, want tamper detection")
	}
}

func TestEncryptedArchiveMetadataAuthentication(t *testing.T) {
	archivePath := createSmallArchive(t)
	encryptedPath := filepath.Join(t.TempDir(), "encrypted.astonish-backup")
	if err := EncryptArchiveFile(archivePath, encryptedPath, "secret"); err != nil {
		t.Fatalf("EncryptArchiveFile() error = %v", err)
	}
	data, err := os.ReadFile(encryptedPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	idx := bytes.IndexByte(data[len(encryptedArchiveMagic):], '\n')
	if idx < 0 {
		t.Fatal("encrypted metadata newline not found")
	}
	metadataStart := len(encryptedArchiveMagic)
	data[metadataStart+idx/2] ^= 0x01
	tamperedPath := filepath.Join(t.TempDir(), "tampered-metadata.astonish-backup")
	if err := os.WriteFile(tamperedPath, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Verify(tamperedPath, ReaderOptions{Passphrase: "secret"}); err == nil {
		t.Fatal("Verify() error = nil, want authenticated metadata failure")
	}
}

func TestArchiveReaderStreamsFiles(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "stream.astonish-backup")
	writer, err := Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := writer.AddFile("platform/users.jsonl", strings.NewReader("{}\n")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Entries = []Entry{{Path: "platform/users.jsonl", Kind: "jsonl", Scope: Scope{Kind: "platform"}, Entity: "users", Records: 1}}
	if err := writer.CloseWithManifest(manifest); err != nil {
		t.Fatalf("CloseWithManifest() error = %v", err)
	}

	reader, err := OpenReader(archivePath)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer reader.Close()
	var paths []string
	if err := reader.ForEachFile(func(path string, r io.Reader) error {
		paths = append(paths, path)
		if path != "platform/users.jsonl" {
			return nil
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		if string(data) != "{}\n" {
			t.Fatalf("streamed payload = %q, want {} newline", data)
		}
		return nil
	}); err != nil {
		t.Fatalf("ForEachFile() error = %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("streamed paths = %v, want payload plus manifest/checksums", paths)
	}
}

func TestCreateRejectsUnknownCompression(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "test.astonish-backup")
	if _, err := Create(archivePath, WriterOptions{Compression: "zip"}); err == nil {
		t.Fatal("Create() error = nil, want unsupported compression error")
	}
}

func TestVerifyDetectsCorruptGzipArchive(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bad.astonish-backup")
	if err := os.WriteFile(archivePath, []byte{0x1f, 0x8b, 0x08, 0x00, 'b', 'a', 'd'}, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Verify(archivePath); err == nil {
		t.Fatal("Verify() error = nil, want corrupt gzip error")
	}
}

func TestVerifyDetectsChecksumMismatch(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bad.astonish-backup")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create file error = %v", err)
	}
	tw := tar.NewWriter(f)
	addTarFile(t, tw, "platform/users.jsonl", []byte("tampered\n"))

	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Entries = []Entry{{Path: "platform/users.jsonl", Kind: "jsonl", Scope: Scope{Kind: "platform"}}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest error = %v", err)
	}
	addTarFile(t, tw, ManifestPath, manifestData)
	checksums := []Checksum{{Path: "platform/users.jsonl", SHA256: strings.Repeat("0", 64), Size: 9}}
	checksumData, err := json.Marshal(checksums)
	if err != nil {
		t.Fatalf("Marshal checksums error = %v", err)
	}
	addTarFile(t, tw, ChecksumsPath, checksumData)
	if err := tw.Close(); err != nil {
		t.Fatalf("Close tar error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close file error = %v", err)
	}

	if _, err := Verify(archivePath); err == nil {
		t.Fatal("Verify() error = nil, want checksum mismatch")
	}
}

func TestWriterRejectsReservedPath(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "test.astonish-backup")
	writer, err := Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer writer.file.Close()

	if _, err := writer.AddFile(ManifestPath, strings.NewReader("{}")); err == nil {
		t.Fatal("AddFile() error = nil, want reserved path error")
	}
}

func createSmallArchive(t *testing.T) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "small.astonish-backup")
	writer, err := Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := writer.AddFile("platform/users.jsonl", strings.NewReader("{}\n")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Entries = []Entry{{Path: "platform/users.jsonl", Kind: "jsonl", Scope: Scope{Kind: "platform"}, Entity: "users", Records: 1}}
	if err := writer.CloseWithManifest(manifest); err != nil {
		t.Fatalf("CloseWithManifest() error = %v", err)
	}
	return archivePath
}

func readEncryptedArchiveParts(t *testing.T, path string) ([]byte, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.HasPrefix(data, []byte(encryptedArchiveMagic)) {
		t.Fatal("archive is not encrypted")
	}
	rest := data[len(encryptedArchiveMagic):]
	idx := bytes.IndexByte(rest, '\n')
	if idx < 0 {
		t.Fatal("encrypted metadata newline not found")
	}
	return rest[:idx], rest[idx+1:]
}

func addTarFile(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
		t.Fatalf("WriteHeader(%s) error = %v", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("Write(%s) error = %v", name, err)
	}
}
