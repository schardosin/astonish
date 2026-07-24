package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/argon2"
)

const encryptedArchiveMagic = "ASTONISH-BACKUP-ENC-v1\n"

var defaultArgon2idParameters = Argon2idParameters{
	MemoryKiB: 64 * 1024,
	Time:      1,
	Threads:   4,
	KeyLen:    32,
}

type ReaderOptions struct {
	Passphrase string
}

type EncryptionInfo struct {
	Version  int                `json:"version"`
	Cipher   string             `json:"cipher"`
	KDF      string             `json:"kdf"`
	Argon2id Argon2idParameters `json:"argon2id,omitempty"`
	Salt     string             `json:"salt"`
	Nonce    string             `json:"nonce"`
}

type Argon2idParameters struct {
	MemoryKiB uint32 `json:"memoryKiB"`
	Time      uint32 `json:"time"`
	Threads   uint8  `json:"threads"`
	KeyLen    uint32 `json:"keyLen"`
}

func EncryptArchiveFile(srcPath, dstPath, passphrase string) error {
	if passphrase == "" {
		return errors.New("backup archive passphrase is required")
	}
	plaintext, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	key := deriveArchiveKey(passphrase, salt, defaultArgon2idParameters)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	info := EncryptionInfo{
		Version:  1,
		Cipher:   "AES-256-GCM",
		KDF:      "argon2id",
		Argon2id: defaultArgon2idParameters,
		Salt:     hex.EncodeToString(salt),
		Nonce:    hex.EncodeToString(nonce),
	}
	metadata, err := json.Marshal(info)
	if err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, metadata)

	var out bytes.Buffer
	out.WriteString(encryptedArchiveMagic)
	out.Write(metadata)
	out.WriteByte('\n')
	out.Write(ciphertext)
	return os.WriteFile(dstPath, out.Bytes(), 0o600)
}

func IsEncryptedArchive(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	prefix := make([]byte, len(encryptedArchiveMagic))
	n, err := io.ReadFull(f, prefix)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	return string(prefix[:n]) == encryptedArchiveMagic, nil
}

func decryptArchivePayload(data []byte, passphrase string) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte(encryptedArchiveMagic)) {
		return data, nil
	}
	if passphrase == "" {
		return nil, errors.New("backup archive is encrypted; passphrase is required")
	}
	rest := data[len(encryptedArchiveMagic):]
	idx := bytes.IndexByte(rest, '\n')
	if idx < 0 {
		return nil, errors.New("encrypted backup archive metadata is missing")
	}
	metadata := rest[:idx]
	ciphertext := rest[idx+1:]
	var info EncryptionInfo
	if err := json.Unmarshal(metadata, &info); err != nil {
		return nil, fmt.Errorf("decode encrypted backup metadata: %w", err)
	}
	params, err := encryptionParameters(info)
	if err != nil {
		return nil, err
	}
	salt, err := hex.DecodeString(info.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode encryption salt: %w", err)
	}
	nonce, err := hex.DecodeString(info.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode encryption nonce: %w", err)
	}
	key := deriveArchiveKey(passphrase, salt, params)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, metadata)
	if err != nil {
		return nil, errors.New("decrypt backup archive: invalid passphrase or corrupted archive")
	}
	return plaintext, nil
}

func encryptionParameters(info EncryptionInfo) (Argon2idParameters, error) {
	if info.Cipher != "AES-256-GCM" {
		return Argon2idParameters{}, fmt.Errorf("unsupported backup encryption cipher %q", info.Cipher)
	}
	if info.KDF == "argon2id:m=65536,t=1,p=4" {
		return defaultArgon2idParameters, nil
	}
	if info.KDF != "argon2id" {
		return Argon2idParameters{}, fmt.Errorf("unsupported backup encryption KDF %q", info.KDF)
	}
	params := info.Argon2id
	if params.MemoryKiB == 0 || params.Time == 0 || params.Threads == 0 || params.KeyLen == 0 {
		return Argon2idParameters{}, errors.New("backup encryption metadata has incomplete argon2id parameters")
	}
	return params, nil
}

func deriveArchiveKey(passphrase string, salt []byte, params Argon2idParameters) []byte {
	return argon2.IDKey([]byte(passphrase), salt, params.Time, params.MemoryKiB, params.Threads, params.KeyLen)
}
