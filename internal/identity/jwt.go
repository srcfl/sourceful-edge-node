package identity

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// GenerateAuthJWT creates an ES256 JWT matching the Blaxt/ESP32 firmware format.
// Header: {"alg":"ES256","typ":"JWT","opr":"auth","device":"{serial}"}
// Payload: {"iat":..., "exp":..., "nonce":"..."}
func (id *Identity) GenerateAuthJWT() (string, error) {
	if id.PrivateKey == nil {
		return "", fmt.Errorf("identity: no private key loaded")
	}

	now := time.Now()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("identity: generate nonce: %w", err)
	}

	header := map[string]string{
		"alg":    "ES256",
		"typ":    "JWT",
		"opr":    "auth",
		"device": id.Serial,
	}

	payload := map[string]any{
		"iat":   now.Unix(),
		"exp":   now.Add(5 * time.Minute).Unix(),
		"nonce": base64.RawURLEncoding.EncodeToString(nonce),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("identity: marshal header: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("identity: marshal payload: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := headerB64 + "." + payloadB64

	// ES256 signature
	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, id.PrivateKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("identity: sign: %w", err)
	}

	// Encode r||s as 64 bytes (32 bytes each, zero-padded)
	sigBytes := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)
	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)

	return signingInput + "." + sigB64, nil
}

// VerifyJWT verifies an ES256 JWT against the given public key.
func VerifyJWT(token string, pub *ecdsa.PublicKey) (bool, error) {
	parts := splitJWT(token)
	if parts == nil {
		return false, fmt.Errorf("invalid JWT format")
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false, fmt.Errorf("decode signature: %w", err)
	}
	if len(sigBytes) != 64 {
		return false, fmt.Errorf("invalid signature length: %d", len(sigBytes))
	}

	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	signingInput := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(signingInput))
	return ecdsa.Verify(pub, hash[:], r, s), nil
}

func splitJWT(token string) []string {
	var parts []string
	start := 0
	count := 0
	for i, c := range token {
		if c == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
			count++
		}
	}
	if count == 2 {
		parts = append(parts, token[start:])
		return parts
	}
	return nil
}
