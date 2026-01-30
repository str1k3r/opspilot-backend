package natsauth

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

type JWTIssuer struct {
	signingKey   nkeys.KeyPair
	accountPubID string
}

func NewJWTIssuer(signingKeySeed, accountPubKey string) (*JWTIssuer, error) {
	kp, err := nkeys.FromSeed([]byte(signingKeySeed))
	if err != nil {
		return nil, fmt.Errorf("invalid NATS signing key seed: %w", err)
	}

	if accountPubKey == "" {
		return nil, fmt.Errorf("missing NATS agents account public key")
	}

	return &JWTIssuer{
		signingKey:   kp,
		accountPubID: accountPubKey,
	}, nil
}

func GenerateUserKeyPair() (seed string, publicKey string, err error) {
	kp, err := nkeys.CreateUser()
	if err != nil {
		return "", "", err
	}

	seedBytes, err := kp.Seed()
	if err != nil {
		return "", "", err
	}

	publicKey, err = kp.PublicKey()
	if err != nil {
		return "", "", err
	}

	return string(seedBytes), publicKey, nil
}

func (ji *JWTIssuer) IssueAgentJWT(agentID, publicKey string, expiresIn time.Duration) (string, time.Time, error) {
	if !nkeys.IsValidPublicUserKey(publicKey) {
		return "", time.Time{}, fmt.Errorf("invalid agent public key")
	}

	now := time.Now().UTC()
	expiresAt := now.Add(expiresIn)

	claims := jwt.NewUserClaims(publicKey)
	claims.IssuedAt = now.Unix()
	claims.Expires = expiresAt.Unix()
	claims.IssuerAccount = ji.accountPubID

	// Allow publishing to agent's event subjects
	claims.Permissions.Pub.Allow.Add("ops." + agentID + ".events.>")
	// Allow publishing to agent's KV entry (heartbeat)
	claims.Permissions.Pub.Allow.Add("$KV.AGENTS." + agentID)
	// Allow KV stream info lookup (required by nats.go KeyValue binding)
	claims.Permissions.Pub.Allow.Add("$JS.API.STREAM.INFO.KV_AGENTS")
	// Allow subscribing to agent's RPC subject
	claims.Permissions.Sub.Allow.Add("ops." + agentID + ".rpc")
	// Allow subscribe to all agent-scoped subjects (safe, still per-agent)
	claims.Permissions.Sub.Allow.Add("ops." + agentID + ".>")
	// Allow subscribing to inbox for request-reply pattern
	claims.Permissions.Sub.Allow.Add("_INBOX.>")
	// Allow publishing to agent-scoped subjects (events + any future channels)
	claims.Permissions.Pub.Allow.Add("ops." + agentID + ".>")

	encoded, err := claims.Encode(ji.signingKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode jwt: %w", err)
	}

	return encoded, expiresAt, nil
}

// BuildCredsFile formats JWT and NKey seed into NATS .creds file format.
//
// This is the standard NATS credentials file format containing both
// the JWT token and the NKey seed in a single file.
func BuildCredsFile(jwtToken, nkeySeed string) string {
	return `-----BEGIN NATS USER JWT-----
` + jwtToken + `
-----END NATS USER JWT-----

-----BEGIN USER NKEY SEED-----
` + nkeySeed + `
-----END USER NKEY SEED-----
`
}

// VerifyNKeySignature verifies agent-signed payload (nonce + timestamp) using NKey public key.
func VerifyNKeySignature(publicKey, nonce string, timestamp int64, signature string) bool {
	if publicKey == "" || nonce == "" || signature == "" || timestamp == 0 {
		return false
	}

	signedData := fmt.Sprintf("%s:%d", nonce, timestamp)

	pubKey, err := nkeys.FromPublicKey(publicKey)
	if err != nil {
		return false
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}

	return pubKey.Verify([]byte(signedData), sigBytes) == nil
}
