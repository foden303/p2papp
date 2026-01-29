package wallet

import (
	"crypto/rand"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Identity represents a peer's identity in the network
type Identity struct {
	PeerID  peer.ID
	PrivKey crypto.PrivKey
	PubKey  crypto.PubKey
}

// GenerateIdentity creates a new Ed25519 identity
func GenerateIdentity() (*Identity, error) {
	privKey, pubKey, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Ed25519 keypair: %w", err)
	}

	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive peer ID: %w", err)
	}

	return &Identity{
		PeerID:  peerID,
		PrivKey: privKey,
		PubKey:  pubKey,
	}, nil
}

// IdentityFromPrivKey creates an identity from an existing private key
func IdentityFromPrivKey(privKey crypto.PrivKey) (*Identity, error) {
	pubKey := privKey.GetPublic()
	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive peer ID: %w", err)
	}

	return &Identity{
		PeerID:  peerID,
		PrivKey: privKey,
		PubKey:  pubKey,
	}, nil
}

// ShortID returns a shortened version of the peer ID for display
func (i *Identity) ShortID() string {
	id := i.PeerID.String()
	if len(id) > 12 {
		return id[:8] + "..." + id[len(id)-4:]
	}
	return id
}

// Sign signs data with the identity's private key
func (i *Identity) Sign(data []byte) ([]byte, error) {
	return i.PrivKey.Sign(data)
}

// Verify verifies a signature against the identity's public key
func (i *Identity) Verify(data, sig []byte) (bool, error) {
	return i.PubKey.Verify(data, sig)
}
