// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"github.com/ava-labs/avalanchego/ids"
)

// TODO: Remove this from this package, this should be in a config file
var EnableCrypto = true

type Factory interface {
	NewPrivateKey() (PrivateKey, error)

	ToPublicKey([]byte) (PublicKey, error)
	ToPrivateKey([]byte) (PrivateKey, error)
}

type RecoverableFactory interface {
	Factory

	RecoverPublicKey(message, signature []byte) (PublicKey, error)
	RecoverHashPublicKey(hash, signature []byte) (PublicKey, error)
}

type PublicKey interface {
	Verify(message, signature []byte) bool
	VerifyHash(hash, signature []byte) bool

	Address() ids.ShortID
	Bytes() []byte
}

type PrivateKey interface {
	PublicKey() PublicKey

	Sign(message []byte) ([]byte, error)
	SignHash(hash []byte) ([]byte, error)

	Bytes() []byte
}
