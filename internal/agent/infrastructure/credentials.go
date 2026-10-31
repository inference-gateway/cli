package infrastructure

import (
	"crypto/ecdsa"
	"crypto/hpke"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// sealInfoPrefix binds a sealed credential blob to its teleport, so a blob
// replayed under another teleport id does not open.
const sealInfoPrefix = "infer-teleport:"

// sealedEnv is the wire shape of SealEnv: the HPKE encapsulated key and the
// ciphertext of the JSON env map.
type sealedEnv struct {
	Enc        []byte `json:"enc"`
	Ciphertext []byte `json:"ciphertext"`
}

// SealEnv encrypts the forwarded environment to the public key of the peer's
// leaf certificate with HPKE, so only the workspace that presented that
// certificate reads it. Keys that are not P-256 cannot seal.
func SealEnv(peer *x509.Certificate, env map[string]string, teleportID string) ([]byte, error) {
	if len(env) == 0 {
		return nil, nil
	}
	ecdsaKey, ok := peer.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("the peer certificate does not carry an ECDSA key")
	}
	ecdhKey, err := ecdsaKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("deriving the peer's key agreement key: %w", err)
	}
	recipient, err := hpke.NewDHKEMPublicKey(ecdhKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	enc, sender, err := hpke.NewSender(recipient, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(sealInfoPrefix+teleportID))
	if err != nil {
		return nil, fmt.Errorf("sealing the credentials: %w", err)
	}
	ciphertext, err := sender.Seal(nil, plaintext)
	if err != nil {
		return nil, fmt.Errorf("sealing the credentials: %w", err)
	}
	return json.Marshal(sealedEnv{Enc: enc, Ciphertext: ciphertext})
}

// OpenEnv decrypts a blob SealEnv produced with the private key behind one of
// the certificates this daemon served.
func OpenEnv(key *ecdsa.PrivateKey, sealed []byte, teleportID string) (map[string]string, error) {
	var blob sealedEnv
	if err := json.Unmarshal(sealed, &blob); err != nil {
		return nil, fmt.Errorf("decoding the sealed credentials: %w", err)
	}
	ecdhKey, err := key.ECDH()
	if err != nil {
		return nil, err
	}
	private, err := hpke.NewDHKEMPrivateKey(ecdhKey)
	if err != nil {
		return nil, err
	}
	recipient, err := hpke.NewRecipient(blob.Enc, private, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(sealInfoPrefix+teleportID))
	if err != nil {
		return nil, fmt.Errorf("opening the credentials: %w", err)
	}
	plaintext, err := recipient.Open(nil, blob.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("opening the credentials: %w", err)
	}
	var env map[string]string
	if err := json.Unmarshal(plaintext, &env); err != nil {
		return nil, err
	}
	return env, nil
}

// keyringSize is how many served certificates a Keyring remembers, so a blob
// sealed to a certificate rotated moments ago still opens.
const keyringSize = 4

// Keyring remembers the private keys of the certificates the binding served
// most recently, newest first.
type Keyring struct {
	mu   sync.Mutex
	keys []*ecdsa.PrivateKey
}

// Remember records the key of a certificate the binding is about to serve.
func (k *Keyring) Remember(cert *tls.Certificate) {
	key, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, known := range k.keys {
		if known.Equal(key) {
			return
		}
	}
	k.keys = append([]*ecdsa.PrivateKey{key}, k.keys...)
	if len(k.keys) > keyringSize {
		k.keys = k.keys[:keyringSize]
	}
}

// Open tries every remembered key on the blob, newest first.
func (k *Keyring) Open(sealed []byte, teleportID string) (map[string]string, error) {
	k.mu.Lock()
	keys := append([]*ecdsa.PrivateKey(nil), k.keys...)
	k.mu.Unlock()
	if len(keys) == 0 {
		return nil, errors.New("no served certificate can open the credentials")
	}
	var err error
	for _, key := range keys {
		var env map[string]string
		if env, err = OpenEnv(key, sealed, teleportID); err == nil {
			return env, nil
		}
	}
	return nil, err
}
