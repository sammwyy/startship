package migrate

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DecryptOpenship opens a value encrypted by Openship's packages/db/src/encryption.ts.
// Format: base64(iv:16 || authTag:16 || ciphertext), AES-256-GCM with sha256(secret).
func DecryptOpenship(sealed string, secrets []string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	packed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sealed))
	if err != nil || len(packed) < 32 {
		return "", errors.New("invalid openship ciphertext")
	}
	iv := packed[:16]
	tag := packed[16:32]
	ct := packed[32:]
	combined := make([]byte, len(ct)+len(tag))
	copy(combined, ct)
	copy(combined[len(ct):], tag)

	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		sum := sha256.Sum256([]byte(secret))
		block, err := aes.NewCipher(sum[:])
		if err != nil {
			continue
		}
		gcm, err := cipher.NewGCMWithNonceSize(block, 16)
		if err != nil {
			continue
		}
		plain, err := gcm.Open(nil, iv, combined, nil)
		if err == nil {
			return string(plain), nil
		}
	}
	return "", errors.New("failed to decrypt openship value with available secrets")
}

// DecryptDokploy opens a value encrypted by Dokploy's packages/server/src/lib/encryption.ts.
// Legacy plaintext values (without "enc:v1:" prefix) are returned unchanged.
// Format: "enc:v1:" + base64(iv:12 || authTag:16 || ciphertext), AES-256-GCM with
// HMAC-SHA256(secret, "dokploy:db-encryption:v1").
func DecryptDokploy(value string, secrets []string, rawKeys [][]byte) (string, error) {
	const prefix = "enc:v1:"
	if value == "" || !strings.HasPrefix(value, prefix) {
		return value, nil
	}
	packed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(value, prefix)))
	if err != nil || len(packed) < 28 {
		return "", errors.New("invalid dokploy ciphertext")
	}
	iv := packed[:12]
	tag := packed[12:28]
	ct := packed[28:]
	combined := make([]byte, len(ct)+len(tag))
	copy(combined, ct)
	copy(combined[len(ct):], tag)

	var keys [][]byte
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte("dokploy:db-encryption:v1"))
		keys = append(keys, mac.Sum(nil))
	}
	keys = append(keys, rawKeys...)

	for _, key := range keys {
		if len(key) != 32 {
			continue
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			continue
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			continue
		}
		plain, err := gcm.Open(nil, iv, combined, nil)
		if err == nil {
			return string(plain), nil
		}
	}
	return "", errors.New("failed to decrypt dokploy value with available keys")
}

// ParseDokployKeyFile parses hex-encoded 32-byte keys from /etc/dokploy/encryption.key.
func ParseDokployKeyFile(content string) [][]byte {
	var out [][]byte
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b, err := hex.DecodeString(line)
		if err == nil && len(b) == 32 {
			out = append(out, b)
		}
	}
	return out
}

// DecryptCoolify opens a value encrypted by Laravel's Crypt (used by Coolify).
// Payload is base64(JSON{"iv": base64, "value": base64, "mac": hex}).
// Plaintext values that are not valid Laravel payloads are returned as-is.
func DecryptCoolify(raw string, appKeys []string) (string, error) {
	if raw == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return raw, nil
	}
	var payload struct {
		IV    string `json:"iv"`
		Value string `json:"value"`
		MAC   string `json:"mac"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil || payload.IV == "" || payload.Value == "" || payload.MAC == "" {
		return raw, nil
	}
	iv, err := base64.StdEncoding.DecodeString(payload.IV)
	if err != nil || len(iv) != aes.BlockSize {
		return "", errors.New("invalid coolify iv")
	}
	ct, err := base64.StdEncoding.DecodeString(payload.Value)
	if err != nil || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", errors.New("invalid coolify ciphertext")
	}
	expectedMAC, err := hex.DecodeString(payload.MAC)
	if err != nil {
		return "", errors.New("invalid coolify mac")
	}

	for _, rawKey := range appKeys {
		key, ok := decodeLaravelKey(rawKey)
		if !ok {
			continue
		}
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(payload.IV + payload.Value))
		if !hmac.Equal(mac.Sum(nil), expectedMAC) {
			continue
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			continue
		}
		plain := make([]byte, len(ct))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
		unpadded, err := pkcs7Unpad(plain, aes.BlockSize)
		if err != nil {
			return "", err
		}
		return unwrapPHPString(string(unpadded)), nil
	}
	return "", fmt.Errorf("failed to decrypt coolify value with available APP_KEYs")
}

func decodeLaravelKey(raw string) ([]byte, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	if b64, ok := strings.CutPrefix(raw, "base64:"); ok {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil || (len(b) != 16 && len(b) != 24 && len(b) != 32) {
			return nil, false
		}
		return b, true
	}
	b := []byte(raw)
	if len(b) != 16 && len(b) != 24 && len(b) != 32 {
		return nil, false
	}
	return b, true
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid padded length")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, errors.New("invalid pkcs7 padding")
	}
	for i := len(data) - pad; i < len(data); i++ {
		if int(data[i]) != pad {
			return nil, errors.New("invalid pkcs7 padding byte")
		}
	}
	return data[:len(data)-pad], nil
}

// unwrapPHPString unwraps PHP serialize() strings like s:6:"secret"; if present.
func unwrapPHPString(s string) string {
	if !strings.HasPrefix(s, "s:") || !strings.HasSuffix(s, `";`) {
		return s
	}
	rest := s[2:]
	colon := strings.Index(rest, `:"`)
	if colon <= 0 {
		return s
	}
	n, err := strconv.Atoi(rest[:colon])
	if err != nil {
		return s
	}
	inner := rest[colon+2 : len(rest)-2]
	if len(inner) == n {
		return inner
	}
	return s
}

// ParseDotEnv parses KEY=VALUE lines (supporting quotes, comments, and `export `).
func ParseDotEnv(content string) map[string]string {
	out := map[string]string{}
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				quote := val[0]
				val = val[1 : len(val)-1]
				if quote == '"' {
					val = strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\"`, `"`, `\\`, `\`).Replace(val)
				}
			}
		}
		if key != "" {
			out[key] = val
		}
	}
	return out
}
