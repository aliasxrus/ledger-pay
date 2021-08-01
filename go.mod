module ledger-pay

go 1.16

require (
	github.com/TRON-US/go-btfs-config v0.10.0
	github.com/btcsuite/btcd v0.22.0-beta // indirect
	github.com/ethereum/go-ethereum v1.10.6 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.4.3
	github.com/google/uuid v1.3.0
	github.com/gorilla/mux v1.8.0
	github.com/klauspost/cpuid/v2 v2.0.8 // indirect
	github.com/libp2p/go-libp2p-core v0.9.0
	github.com/multiformats/go-multiaddr v0.4.0 // indirect
	github.com/multiformats/go-multihash v0.0.15 // indirect
	github.com/tron-us/go-btfs-common v0.7.25
	github.com/tron-us/protobuf v1.3.7
	github.com/tyler-smith/go-bip32 v1.0.0
	github.com/tyler-smith/go-bip39 v1.1.0
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
)

replace github.com/ipfs/go-cid => github.com/TRON-US/go-cid v0.3.0

replace github.com/libp2p/go-libp2p-core => github.com/TRON-US/go-libp2p-core v0.7.1

replace github.com/multiformats/go-multiaddr => github.com/TRON-US/go-multiaddr v0.4.0
