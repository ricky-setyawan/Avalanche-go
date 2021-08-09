// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"crypto/tls"
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/profiler"
)

// Config contains all of the configurations of an Avalanche node.
type Config struct {
	genesis.Params

	// Genesis information
	GenesisBytes []byte `json:"-"`
	AvaxAssetID  ids.ID `json:"avaxAssetID"`

	// protocol to use for opening the network interface
	Nat nat.Router `json:"-"`

	// Attempted NAT Traversal did we attempt
	AttemptedNATTraversal bool `json:"attemptedNATTraversal"`

	// ID of the network this node should connect to
	NetworkID uint32 `json:"networkID"`

	// Assertions configuration
	EnableAssertions bool `json:"enableAssertions"`

	// Crypto configuration
	EnableCrypto bool `json:"enableCrypto"`

	// Path to database
	DBPath string `json:"dbPath"`

	// Name of the database type to use
	DBName string `json:"dbName"`

	// Staking configuration
	StakingIP             utils.DynamicIPDesc `json:"stakingIP"`
	EnableStaking         bool                `json:"enableStaking"`
	StakingTLSCert        tls.Certificate     `json:"-"`
	DisabledStakingWeight uint64              `json:"disabledStakingWeight"`

	// Health
	HealthCheckFreq time.Duration `json:"healthCheckFreq"`

	// Network configuration
	NetworkConfig      network.Config `json:"networkConfig"`
	PeerListSize       uint32         `json:"peerListSize"`
	PeerListGossipSize uint32         `json:"peerListGossipSize"`
	PeerListGossipFreq time.Duration  `json:"peerListGossipFreq"`
	CompressionEnabled bool           `json:"compressionEnabled"`

	// Benchlist Configuration
	BenchlistConfig benchlist.Config `json:"benchlistConfig"`

	// Bootstrapping configuration
	BootstrapIDs []ids.ShortID  `json:"bootstrapIDs"`
	BootstrapIPs []utils.IPDesc `json:"bootstrapIPs"`

	// HTTP configuration
	HTTPHost string `json:"httpHost"`
	HTTPPort uint16 `json:"httpPort"`

	HTTPSEnabled bool `json:"httpsEnabled"`
	// TODO pass the HTTPS certificate in the way we pass in StakingTLSCert
	HTTPSKeyFile        string   `json:"httpsKeyFile"`
	HTTPSCertFile       string   `json:"httpsCertFile"`
	StakingKeyFile      string   `json:"stakingKeyFile"`  // TODO populate
	StakingCertFile     string   `json:"stakingCertFile"` // TODO populate
	APIRequireAuthToken bool     `json:"apiRequireAuthToken"`
	APIAuthPassword     string   `json:"-"`
	APIAllowedOrigins   []string `json:"apiAllowedOrigins"`

	// Enable/Disable APIs
	AdminAPIEnabled    bool `json:"adminAPIEnabled"`
	InfoAPIEnabled     bool `json:"infoAPIEnabled"`
	KeystoreAPIEnabled bool `json:"keystoreAPIEnabled"`
	MetricsAPIEnabled  bool `json:"metricsAPIEnabled"`
	HealthAPIEnabled   bool `json:"healthAPIEnabled"`
	IndexAPIEnabled    bool `json:"indexAPIEnabled"`

	// Profiling configurations
	ProfilerConfig profiler.Config `json:"profilerConfig"`

	// Logging configuration
	LoggingConfig logging.Config `json:"loggingConfig"`

	// Plugin directory
	PluginDir string `json:"pluginDir"`

	// Consensus configuration
	ConsensusParams avalanche.Parameters `json:"consensusParams"`

	// IPC configuration
	IPCAPIEnabled      bool     `json:"ipcAPIEnabled"`
	IPCPath            string   `json:"ipcPath"`
	IPCDefaultChainIDs []string `json:"ipcDefaultChainIDs"`

	// Metrics
	MeterVMEnabled bool `json:"meterVMEnabled"`

	// Router that is used to handle incoming consensus messages
	ConsensusRouter          router.Router       `json:"-"`
	RouterHealthConfig       router.HealthConfig `json:"routerHealthConfig"`
	ConsensusShutdownTimeout time.Duration       `json:"consensusShutdownTimeout"`
	ConsensusGossipFrequency time.Duration       `json:"consensusGossipFrequency"`
	// Number of peers to gossip to when gossiping accepted frontier
	ConsensusGossipAcceptedFrontierSize uint `json:"consensusGossipAcceptedFrontierSize"`
	// Number of peers to gossip each accepted container to
	ConsensusGossipOnAcceptSize uint `json:"consensusGossipOnAcceptSize"`

	// Dynamic Update duration for IP or NAT traversal
	DynamicUpdateDuration time.Duration `json:"dynamicUpdateDuration"`

	DynamicPublicIPResolver dynamicip.Resolver `json:"-"`

	// Subnet Whitelist
	WhitelistedSubnets ids.Set `json:"whitelistedSubnets"`

	IndexAllowIncomplete bool `json:"indexAllowIncomplete"`

	// Should Bootstrap be retried
	RetryBootstrap bool `json:"retryBootstrap"`

	// Max number of times to retry bootstrap
	RetryBootstrapMaxAttempts int `json:"retryBootstrapMaxAttempts"`

	// Timeout when connecting to bootstrapping beacons
	BootstrapBeaconConnectionTimeout time.Duration `json:"bootstrapBeaconConnectionTimeout"`

	// Max number of containers in a multiput message sent by this node.
	BootstrapMultiputMaxContainersSent int `json:"bootstrapMultiputMaxContainersSent"`

	// This node will only consider the first [MultiputMaxContainersReceived]
	// containers in a multiput it receives.
	BootstrapMultiputMaxContainersReceived int `json:"bootstrapMultiputMaxContainersReceived"`

	// Peer alias configuration
	PeerAliasTimeout time.Duration `json:"peerAliasTimeout"`

	// ChainConfigs
	ChainConfigs map[string]chains.ChainConfig `json:"chainConfigs"`

	// Max time to spend fetching a container and its
	// ancestors while responding to a GetAncestors message
	BootstrapMaxTimeGetAncestors time.Duration `json:"bootstrapMaxTimeGetAncestors"`

	// VM Aliases
	VMAliases map[ids.ID][]string `json:"vmAliases"`
}
