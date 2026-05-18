package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMasterKey_Generate(t *testing.T) {
	dir := t.TempDir()

	key, err := InitMasterKey(dir)

	require.NoError(t, err)
	assert.Len(t, key, 32)

	data, err := os.ReadFile(filepath.Join(dir, "master.key"))
	require.NoError(t, err)
	assert.Equal(t, key, data)
}

func TestInitMasterKey_LoadExisting(t *testing.T) {
	dir := t.TempDir()

	key1, err := InitMasterKey(dir)
	require.NoError(t, err)

	key2, err := InitMasterKey(dir)

	require.NoError(t, err)
	assert.Equal(t, key1, key2)
}

func TestInitMasterKey_InvalidSize(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "master.key"), []byte("too-short"), 0600))

	_, err := InitMasterKey(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid master.key")
}

func TestEncryptDecrypt(t *testing.T) {
	key := testKey(0)
	plaintext := "super-secret-rclone-credential"

	encrypted, err := Encrypt(plaintext, key)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, encrypted)

	decrypted, err := Decrypt(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptDecrypt_DifferentNonces(t *testing.T) {
	key := testKey(0)

	enc1, err := Encrypt("same-text", key)
	require.NoError(t, err)

	enc2, err := Encrypt("same-text", key)
	require.NoError(t, err)

	assert.NotEqual(t, enc1, enc2, "random nonce should produce different ciphertexts")
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := testKey(0)
	key2 := testKey(1)

	encrypted, err := Encrypt("secret", key1)
	require.NoError(t, err)

	_, err = Decrypt(encrypted, key2)
	assert.Error(t, err)
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key := testKey(0)

	_, err := Decrypt("not-valid-base64!!!", key)

	assert.Error(t, err)
}

func TestEncryptDecrypt_EmptyString(t *testing.T) {
	key := testKey(0)

	encrypted, err := Encrypt("", key)
	require.NoError(t, err)

	decrypted, err := Decrypt(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, "", decrypted)
}

func TestEncryptDecrypt_LongString(t *testing.T) {
	key := testKey(0)
	longText := strings.Repeat("abcdefghij", 1000)

	encrypted, err := Encrypt(longText, key)
	require.NoError(t, err)

	decrypted, err := Decrypt(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, longText, decrypted)
}

func TestMasterKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()

	_, err := InitMasterKey(dir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, "master.key"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func testKey(offset byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i) + offset
	}
	return key
}
