package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	ManifestPath  = "manifest.json"
	ChecksumsPath = "checksums.json"
)

type Compression string

const (
	CompressionGzip Compression = "gzip"
	CompressionNone Compression = "none"
)

type WriterOptions struct {
	Compression Compression
}

type Checksum struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Summary struct {
	Manifest  Manifest   `json:"manifest"`
	Checksums []Checksum `json:"checksums"`
}

type Writer struct {
	file        *os.File
	tar         *tar.Writer
	compressor  io.Closer
	compression Compression
	entries     []Checksum
	closed      bool
}

func Create(path string, opts ...WriterOptions) (*Writer, error) {
	options := WriterOptions{Compression: CompressionGzip}
	if len(opts) > 0 {
		options = opts[0]
	}
	compression, err := normalizeCompression(options.Compression)
	if err != nil {
		return nil, err
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	var out io.Writer = f
	var compressor io.Closer
	if compression == CompressionGzip {
		gz := gzip.NewWriter(f)
		out = gz
		compressor = gz
	}
	return &Writer{file: f, tar: tar.NewWriter(out), compressor: compressor, compression: compression}, nil
}

func ParseCompression(value string) (Compression, error) {
	return normalizeCompression(Compression(value))
}

func (w *Writer) AddFile(path string, r io.Reader) (Checksum, error) {
	if w.closed {
		return Checksum{}, errors.New("backup archive writer is closed")
	}
	if err := validateArchivePath(path); err != nil {
		return Checksum{}, err
	}
	if path == ManifestPath || path == ChecksumsPath {
		return Checksum{}, fmt.Errorf("%s is reserved", path)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return Checksum{}, err
	}
	checksum := checksumFor(path, data)
	if err := w.writeTarFile(path, data); err != nil {
		return Checksum{}, err
	}
	w.entries = append(w.entries, checksum)
	return checksum, nil
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.closeArchive()
}

func (w *Writer) CloseWithManifest(manifest Manifest) error {
	if w.closed {
		return errors.New("backup archive writer is closed")
	}
	w.closed = true
	defer w.file.Close()

	manifest.Compression = string(w.compression)
	if err := manifest.Validate(); err != nil {
		_ = w.tar.Close()
		if w.compressor != nil {
			_ = w.compressor.Close()
		}
		return err
	}
	checksums := append([]Checksum(nil), w.entries...)
	sort.Slice(checksums, func(i, j int) bool { return checksums[i].Path < checksums[j].Path })

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = w.tar.Close()
		if w.compressor != nil {
			_ = w.compressor.Close()
		}
		return err
	}
	if err := w.writeTarFile(ManifestPath, append(manifestData, '\n')); err != nil {
		_ = w.tar.Close()
		if w.compressor != nil {
			_ = w.compressor.Close()
		}
		return err
	}

	checksumData, err := json.MarshalIndent(checksums, "", "  ")
	if err != nil {
		_ = w.tar.Close()
		if w.compressor != nil {
			_ = w.compressor.Close()
		}
		return err
	}
	if err := w.writeTarFile(ChecksumsPath, append(checksumData, '\n')); err != nil {
		_ = w.tar.Close()
		if w.compressor != nil {
			_ = w.compressor.Close()
		}
		return err
	}

	if err := w.tar.Close(); err != nil {
		return err
	}
	if w.compressor != nil {
		if err := w.compressor.Close(); err != nil {
			return err
		}
	}
	return w.file.Close()
}

func Inspect(path string, opts ...ReaderOptions) (Summary, error) {
	files, err := ReadArchiveFiles(path, opts...)
	if err != nil {
		return Summary{}, err
	}
	manifestData, ok := files[ManifestPath]
	if !ok {
		return Summary{}, errors.New("backup archive missing manifest.json")
	}
	checksumData, ok := files[ChecksumsPath]
	if !ok {
		return Summary{}, errors.New("backup archive missing checksums.json")
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return Summary{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Summary{}, err
	}

	var checksums []Checksum
	if err := json.Unmarshal(checksumData, &checksums); err != nil {
		return Summary{}, fmt.Errorf("decode checksums: %w", err)
	}
	return Summary{Manifest: manifest, Checksums: checksums}, nil
}

func Verify(path string, opts ...ReaderOptions) (Summary, error) {
	files, err := ReadArchiveFiles(path, opts...)
	if err != nil {
		return Summary{}, err
	}
	summary, err := Inspect(path, opts...)
	if err != nil {
		return Summary{}, err
	}
	seen := make(map[string]struct{}, len(summary.Checksums))
	for _, checksum := range summary.Checksums {
		if err := checksum.Validate(); err != nil {
			return Summary{}, err
		}
		data, ok := files[checksum.Path]
		if !ok {
			return Summary{}, fmt.Errorf("backup archive missing checksummed file %q", checksum.Path)
		}
		got := checksumFor(checksum.Path, data)
		if got.SHA256 != checksum.SHA256 || got.Size != checksum.Size {
			return Summary{}, fmt.Errorf("checksum mismatch for %q", checksum.Path)
		}
		seen[checksum.Path] = struct{}{}
	}
	for path := range files {
		if path == ManifestPath || path == ChecksumsPath {
			continue
		}
		if _, ok := seen[path]; !ok {
			return Summary{}, fmt.Errorf("backup archive contains unchecked file %q", path)
		}
	}
	return summary, nil
}

func (c Checksum) Validate() error {
	if err := validateArchivePath(c.Path); err != nil {
		return err
	}
	if c.Path == ManifestPath || c.Path == ChecksumsPath {
		return fmt.Errorf("checksum path %q is reserved", c.Path)
	}
	if c.Size < 0 {
		return fmt.Errorf("checksum size for %q cannot be negative", c.Path)
	}
	if len(c.SHA256) != sha256.Size*2 {
		return fmt.Errorf("checksum for %q must be a SHA-256 hex digest", c.Path)
	}
	if _, err := hex.DecodeString(c.SHA256); err != nil {
		return fmt.Errorf("checksum for %q must be hex: %w", c.Path, err)
	}
	return nil
}

func (w *Writer) writeTarFile(path string, data []byte) error {
	hdr := &tar.Header{
		Name:    path,
		Mode:    0o600,
		Size:    int64(len(data)),
		ModTime: time.Now().UTC(),
	}
	if err := w.tar.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := w.tar.Write(data)
	return err
}

func (w *Writer) closeArchive() error {
	if err := w.tar.Close(); err != nil {
		_ = w.closeCompressorAndFile()
		return err
	}
	return w.closeCompressorAndFile()
}

func (w *Writer) closeCompressorAndFile() error {
	if w.compressor != nil {
		if err := w.compressor.Close(); err != nil {
			_ = w.file.Close()
			return err
		}
	}
	return w.file.Close()
}

func ReadArchiveFiles(path string, opts ...ReaderOptions) (map[string][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	options := ReaderOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}
	data, err = decryptArchivePayload(data, options.Passphrase)
	if err != nil {
		return nil, err
	}

	r, closePayload, err := archivePayloadReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if closePayload != nil {
		defer closePayload()
	}

	files := make(map[string][]byte)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("backup archive contains non-file entry %q", hdr.Name)
		}
		if err := validateArchivePath(hdr.Name); err != nil {
			return nil, err
		}
		if _, exists := files[hdr.Name]; exists {
			return nil, fmt.Errorf("backup archive contains duplicate file %q", hdr.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		files[hdr.Name] = data
	}
	return files, nil
}

func archivePayloadReader(r io.Reader) (io.Reader, func(), error) {
	br := bufio.NewReader(r)
	peek, err := br.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, nil, err
	}
	if len(peek) == 2 && peek[0] == 0x1f && peek[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, nil, fmt.Errorf("open gzip-compressed backup archive: %w", err)
		}
		return gz, func() { _ = gz.Close() }, nil
	}
	return br, nil, nil
}

func normalizeCompression(compression Compression) (Compression, error) {
	if compression == "" {
		return CompressionGzip, nil
	}
	switch compression {
	case CompressionGzip, CompressionNone:
		return compression, nil
	default:
		return "", fmt.Errorf("unsupported backup compression %q", compression)
	}
}

func checksumFor(path string, data []byte) Checksum {
	sum := sha256.Sum256(data)
	return Checksum{Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

func validateArchivePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("archive path is required")
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return fmt.Errorf("archive path %q must be relative and must not contain parent traversal", path)
	}
	return nil
}
