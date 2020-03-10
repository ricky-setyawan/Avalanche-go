// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ava-labs/coreth/core"

	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/params"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/evm"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/ava-labs/gecko/vms/timestampvm"
)

// Note that since an AVA network has exactly one Platform Chain,
// and the Platform Chain defines the genesis state of the network
// (who is staking, which chains exist, etc.), defining the genesis
// state of the Platform Chain is the same as defining the genesis
// state of the network.

// Hardcoded network IDs
const (
	MainnetID  uint32 = 1
	TestnetID  uint32 = 2
	BorealisID uint32 = 2
	LocalID    uint32 = 12345

	MainnetName  = "mainnet"
	TestnetName  = "testnet"
	BorealisName = "borealis"
	LocalName    = "local"
)

var (
	validNetworkName = regexp.MustCompile(`network-[0-9]+`)
)

// Hard coded genesis constants
var (
	// Give special names to the mainnet and testnet
	NetworkIDToNetworkName = map[uint32]string{
		MainnetID: MainnetName,
		TestnetID: BorealisName,
		LocalID:   LocalName,
	}
	NetworkNameToNetworkID = map[string]uint32{
		MainnetName:  MainnetID,
		TestnetName:  TestnetID,
		BorealisName: BorealisID,
		LocalName:    LocalID,
	}
	Keys = []string{
		"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
	}
	Addresses = []string{
		"6Y3kysjF9jnHnYkdS9yGAuoHyae2eNmeV",
	}
	ParsedAddresses = []ids.ShortID{}
	StakerIDs       = []string{
		"7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
		"MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ",
		"NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN",
		"GWPcbFJZFfZreETSoWjPimr846mXEKCtu",
		"P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5",
	}
	ParsedStakerIDs = []ids.ShortID{}
)

func init() {
	for _, addrStr := range Addresses {
		addr, err := ids.ShortFromString(addrStr)
		if err != nil {
			panic(err)
		}
		ParsedAddresses = append(ParsedAddresses, addr)
	}
	for _, stakerIDStr := range StakerIDs {
		stakerID, err := ids.ShortFromString(stakerIDStr)
		if err != nil {
			panic(err)
		}
		ParsedStakerIDs = append(ParsedStakerIDs, stakerID)
	}
}

// NetworkName returns a human readable name for the network with
// ID [networkID]
func NetworkName(networkID uint32) string {
	if name, exists := NetworkIDToNetworkName[networkID]; exists {
		return name
	}
	return fmt.Sprintf("network-%d", networkID)
}

// NetworkID returns the ID of the network with name [networkName]
func NetworkID(networkName string) (uint32, error) {
	networkName = strings.ToLower(networkName)
	if id, exists := NetworkNameToNetworkID[networkName]; exists {
		return id, nil
	}

	if id, err := strconv.ParseUint(networkName, 10, 0); err == nil {
		if id > math.MaxUint32 {
			return 0, fmt.Errorf("NetworkID %s not in [0, 2^32)", networkName)
		}
		return uint32(id), nil
	}
	if validNetworkName.MatchString(networkName) {
		if id, err := strconv.Atoi(networkName[8:]); err == nil {
			if id > math.MaxUint32 {
				return 0, fmt.Errorf("NetworkID %s not in [0, 2^32)", networkName)
			}
			return uint32(id), nil
		}
	}

	return 0, fmt.Errorf("Failed to parse %s as a network name", networkName)
}

// Aliases returns the default aliases based on the network ID
func Aliases(networkID uint32) (generalAliases map[string][]string, chainAliases map[[32]byte][]string, vmAliases map[[32]byte][]string) {
	generalAliases = map[string][]string{
		"vm/" + platformvm.ID.String():  []string{"vm/platform"},
		"vm/" + avm.ID.String():         []string{"vm/avm"},
		"vm/" + evm.ID.String():         []string{"vm/evm"},
		"vm/" + spdagvm.ID.String():     []string{"vm/spdag"},
		"vm/" + spchainvm.ID.String():   []string{"vm/spchain"},
		"vm/" + timestampvm.ID.String(): []string{"vm/timestamp"},
		"bc/" + ids.Empty.String():      []string{"P", "platform", "bc/P", "bc/platform"},
	}
	chainAliases = map[[32]byte][]string{
		ids.Empty.Key(): []string{"P", "platform"},
	}
	vmAliases = map[[32]byte][]string{
		platformvm.ID.Key():  []string{"platform"},
		avm.ID.Key():         []string{"avm"},
		evm.ID.Key():         []string{"evm"},
		spdagvm.ID.Key():     []string{"spdag"},
		spchainvm.ID.Key():   []string{"spchain"},
		timestampvm.ID.Key(): []string{"timestamp"},
	}

	genesisBytes, _ := Genesis(networkID)
	genesis := &platformvm.Genesis{}                  // TODO let's not re-create genesis to do aliasing
	platformvm.Codec.Unmarshal(genesisBytes, genesis) // TODO check for error
	genesis.Initialize()

	for _, chain := range genesis.Chains {
		switch {
		case avm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"X", "avm", "bc/X", "bc/avm"}
			chainAliases[chain.ID().Key()] = []string{"X", "avm"}
		case evm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"C", "evm", "bc/C", "bc/evm"}
			chainAliases[chain.ID().Key()] = []string{"C", "evm"}
		case spdagvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/spdag"}
			chainAliases[chain.ID().Key()] = []string{"spdag"}
		case spchainvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/spchain"}
			chainAliases[chain.ID().Key()] = []string{"spchain"}
		case timestampvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/timestamp"}
			chainAliases[chain.ID().Key()] = []string{"timestamp"}
		}
	}
	return
}

// Genesis returns the genesis data of the Platform Chain.
// Since the Platform Chain causes the creation of all other
// chains, this function returns the genesis data of the entire network.
// The ID of the new network is [networkID].
func Genesis(networkID uint32) ([]byte, error) {
	// Specify the genesis state of the AVM
	avmArgs := avm.BuildGenesisArgs{}
	{
		holders := []interface{}(nil)
		for _, addr := range Addresses {
			holders = append(holders, avm.Holder{
				Amount:  json.Uint64(45 * units.MegaAva),
				Address: addr,
			})
		}
		avmArgs.GenesisData = map[string]avm.AssetDefinition{
			// The AVM starts out with one asset, $AVA
			"AVA": avm.AssetDefinition{
				Name:         "AVA",
				Symbol:       "AVA",
				Denomination: 9,
				InitialState: map[string][]interface{}{
					"fixedCap": holders,
				},
			},
		}
	}
	avmReply := avm.BuildGenesisReply{}

	avmSS := avm.StaticService{}
	err := avmSS.BuildGenesis(nil, &avmArgs, &avmReply)
	if err != nil {
		panic(err)
	}

	// Specify the genesis state of Athereum (the built-in instance of the EVM)
	evmBalance, success := new(big.Int).SetString("33b2e3c9fd0804000000000", 16)
	if success != true {
		return nil, errors.New("problem creating evm genesis state")
	}
	evmArgs := core.Genesis{
		Config: &params.ChainConfig{
			ChainID:             big.NewInt(43110),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
		},
		Nonce:      0,
		Timestamp:  0,
		ExtraData:  []byte{0},
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Mixhash:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Alloc: core.GenesisAlloc{
			common.HexToAddress(evm.GenesisTestAddr): core.GenesisAccount{
				Balance: evmBalance,
			},
		},
		Number:     0,
		GasUsed:    0,
		ParentHash: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
	}
	evmSS := evm.StaticService{}
	evmReply, err := evmSS.BuildGenesis(nil, &evmArgs)
	if err != nil {
		return nil, err
	}

	// Specify the genesis state of the simple payments DAG
	spdagvmArgs := spdagvm.BuildGenesisArgs{}
	for _, addr := range ParsedAddresses {
		spdagvmArgs.Outputs = append(spdagvmArgs.Outputs,
			spdagvm.APIOutput{
				Amount:    json.Uint64(20 * units.KiloAva),
				Threshold: 1,
				Addresses: []ids.ShortID{addr},
			},
		)
	}

	spdagvmReply := spdagvm.BuildGenesisReply{}
	spdagvmSS := spdagvm.StaticService{}
	if err := spdagvmSS.BuildGenesis(nil, &spdagvmArgs, &spdagvmReply); err != nil {
		return nil, fmt.Errorf("problem creating simple payments DAG: %w", err)
	}

	// Specify the genesis state of the simple payments chain
	spchainvmArgs := spchainvm.BuildGenesisArgs{}
	for _, addr := range ParsedAddresses {
		spchainvmArgs.Accounts = append(spchainvmArgs.Accounts,
			spchainvm.APIAccount{
				Address: addr,
				Balance: json.Uint64(20 * units.KiloAva),
			},
		)
	}
	spchainvmReply := spchainvm.BuildGenesisReply{}

	spchainvmSS := spchainvm.StaticService{}
	if err := spchainvmSS.BuildGenesis(nil, &spchainvmArgs, &spchainvmReply); err != nil {
		return nil, fmt.Errorf("problem creating simple payments chain: %w", err)
	}

	// Specify the initial state of the Platform Chain
	platformvmArgs := platformvm.BuildGenesisArgs{
		NetworkID: json.Uint32(networkID),
	}
	for _, addr := range ParsedAddresses {
		platformvmArgs.Accounts = append(platformvmArgs.Accounts,
			platformvm.APIAccount{
				Address: addr,
				Balance: json.Uint64(20 * units.KiloAva),
			},
		)
	}

	genesisTime := time.Date(
		/*year=*/ 2019,
		/*month=*/ time.November,
		/*day=*/ 1,
		/*hour=*/ 0,
		/*minute=*/ 0,
		/*second=*/ 0,
		/*nano-second=*/ 0,
		/*location=*/ time.UTC,
	)
	stakingDuration := 365 * 24 * time.Hour // ~ 1 year
	endStakingTime := genesisTime.Add(stakingDuration)

	for i, validatorID := range ParsedStakerIDs {
		weight := json.Uint64(20 * units.KiloAva)
		platformvmArgs.Validators = append(platformvmArgs.Validators,
			platformvm.APIDefaultSubnetValidator{
				APIValidator: platformvm.APIValidator{
					StartTime: json.Uint64(genesisTime.Unix()),
					EndTime:   json.Uint64(endStakingTime.Unix()),
					Weight:    &weight,
					ID:        validatorID,
				},
				Destination: ParsedAddresses[i%len(ParsedAddresses)],
			},
		)
	}

	// Specify the chains that exist upon this network's creation
	platformvmArgs.Chains = []platformvm.APIChain{
		platformvm.APIChain{
			GenesisData: avmReply.Bytes,
			VMID:        avm.ID,
			FxIDs: []ids.ID{
				secp256k1fx.ID,
			},
			Name: "AVM",
		},
		platformvm.APIChain{
			GenesisData: evmReply,
			VMID:        evm.ID,
			Name:        "Athereum",
		},
		platformvm.APIChain{
			GenesisData: spdagvmReply.Bytes,
			VMID:        spdagvm.ID,
			Name:        "Simple DAG Payments",
		},
		platformvm.APIChain{
			GenesisData: spchainvmReply.Bytes,
			VMID:        spchainvm.ID,
			Name:        "Simple Chain Payments",
		},
		platformvm.APIChain{
			GenesisData: formatting.CB58{Bytes: []byte{}}, // There is no genesis data
			VMID:        timestampvm.ID,
			Name:        "Simple Timestamp Server",
		},
	}

	platformvmArgs.Time = json.Uint64(genesisTime.Unix())
	platformvmReply := platformvm.BuildGenesisReply{}

	platformvmSS := platformvm.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &platformvmArgs, &platformvmReply); err != nil {
		return nil, fmt.Errorf("problem while building platform chain's genesis state: %w", err)
	}

	return platformvmReply.Bytes.Bytes, nil
}

// VMGenesis ...
func VMGenesis(networkID uint32, vmID ids.ID) *platformvm.CreateChainTx {
	genesisBytes, _ := Genesis(networkID)
	genesis := platformvm.Genesis{}
	platformvm.Codec.Unmarshal(genesisBytes, &genesis)
	for _, chain := range genesis.Chains {
		if chain.VMID.Equals(vmID) {
			return chain
		}
	}
	return nil
}
