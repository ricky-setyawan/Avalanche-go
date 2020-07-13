// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

func TestNetworkName(t *testing.T) {
	if name := NetworkName(MainnetID); name != MainnetName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, MainnetName)
	}
	if name := NetworkName(CascadeID); name != CascadeName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, CascadeName)
	}
	if name := NetworkName(DenaliID); name != DenaliName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, DenaliName)
	}
	if name := NetworkName(EverestID); name != EverestName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, EverestName)
	}
	if name := NetworkName(TestnetID); name != EverestName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, EverestName)
	}
	if name := NetworkName(4294967295); name != "network-4294967295" {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, "network-4294967295")
	}
}

func TestNetworkID(t *testing.T) {
	id, err := NetworkID(MainnetName)
	if err != nil {
		t.Fatal(err)
	}
	if id != MainnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", MainnetID, id)
	}

	id, err = NetworkID(CascadeName)
	if err != nil {
		t.Fatal(err)
	}
	if id != CascadeID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", CascadeID, id)
	}

	id, err = NetworkID("cAsCaDe")
	if err != nil {
		t.Fatal(err)
	}
	if id != CascadeID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", CascadeID, id)
	}

	id, err = NetworkID(DenaliName)
	if err != nil {
		t.Fatal(err)
	}
	if id != DenaliID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", DenaliID, id)
	}

	id, err = NetworkID("dEnAlI")
	if err != nil {
		t.Fatal(err)
	}
	if id != DenaliID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", DenaliID, id)
	}

	id, err = NetworkID(TestnetName)
	if err != nil {
		t.Fatal(err)
	}
	if id != TestnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", TestnetID, id)
	}

	id, err = NetworkID("network-4294967295")
	if err != nil {
		t.Fatal(err)
	}
	if id != 4294967295 {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", 4294967295, id)
	}

	id, err = NetworkID("4294967295")
	if err != nil {
		t.Fatal(err)
	}
	if id != 4294967295 {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", 4294967295, id)
	}

	if _, err := NetworkID("network-4294967296"); err == nil {
		t.Fatalf("Should have errored due to the network being too large.")
	}

	if _, err := NetworkID("4294967296"); err == nil {
		t.Fatalf("Should have errored due to the network being too large.")
	}

	if _, err := NetworkID("asdcvasdc-252"); err == nil {
		t.Fatalf("Should have errored due to the invalid input string.")
	}
}

func TestAliases(t *testing.T) {
	generalAliases, _, _, _ := Aliases(LocalID)
	if _, exists := generalAliases["vm/"+platformvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+avm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+EVMID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+spdagvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+spchainvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	}
}

func TestGenesis(t *testing.T) {
	genesisBytes, _, err := Genesis(LocalID)
	if err != nil {
		t.Fatal(err)
	}
	genesis := platformvm.Genesis{}
	if err := platformvm.Codec.Unmarshal(genesisBytes, &genesis); err != nil {
		t.Fatal(err)
	}
}

func TestVMGenesis(t *testing.T) {
	tests := []struct {
		networkID  uint32
		vmID       ids.ID
		expectedID string
	}{
		{
			networkID:  EverestID,
			vmID:       avm.ID,
			expectedID: "2Bp1Re3JqrJYUG1Dxy8tEa7e7nCBaCQ4m9sbN5xhknK9rtL94q",
		},
		{
			networkID:  DenaliID,
			vmID:       avm.ID,
			expectedID: "2JS1yvaaSmUCt7kcNX1cgfFFmyLCwn9Cn5qKnH2FxTvQvqETSe",
		},
		{
			networkID:  CascadeID,
			vmID:       avm.ID,
			expectedID: "2WBZpeGrBpy4RHFkaUQazQdL2teCPd3WqejVzHoHbfhNWqippH",
		},
		{
			networkID:  LocalID,
			vmID:       avm.ID,
			expectedID: "2nvFvaf8zjxKAeoWVnSj8H216dYussYsTNzS4ym7FYpPaMPUdp",
		},
		{
			networkID:  EverestID,
			vmID:       EVMID,
			expectedID: "SmQ8LvasSgPrPtUhQ6MaL2JNdeTp1otZ6G9pDM3a78jqzhzs6",
		},
		{
			networkID:  DenaliID,
			vmID:       EVMID,
			expectedID: "eZamFmqytbwyF3bTWXDTfV5uRS2Q1y28ETMLa4yULYviLumDm",
		},
		{
			networkID:  CascadeID,
			vmID:       EVMID,
			expectedID: "21CSKU23JjT1XEDUAP8X4qPwMn5U287SV47UyXyWT2cmyq2vVk",
		},
		{
			networkID:  LocalID,
			vmID:       EVMID,
			expectedID: "wq1BBr15MMmwTCJ2Q2ggUeWFbeuecHzxdTKuAyyftKgLRrc3q",
		},
	}

	for _, test := range tests {
		genesisTx, err := VMGenesis(test.networkID, test.vmID)
		if err != nil {
			t.Fatal(err)
		}
		if result := genesisTx.ID().String(); test.expectedID != result {
			t.Fatalf("%s genesisID with networkID %d was expected to be %s but was %s",
				test.vmID,
				test.networkID,
				test.expectedID,
				result)
		}
	}
}

func TestAVAXAssetID(t *testing.T) {
	tests := []struct {
		networkID  uint32
		expectedID string
	}{
		{
			networkID:  EverestID,
			expectedID: "2CUYXeGx3cXXA91NRHzDhNKQXqPB8TnDDQPg75zRAXUgTmaoRx",
		},
		{
			networkID:  DenaliID,
			expectedID: "2CUYXeGx3cXXA91NRHzDhNKQXqPB8TnDDQPg75zRAXUgTmaoRx",
		},
		{
			networkID:  CascadeID,
			expectedID: "2CUYXeGx3cXXA91NRHzDhNKQXqPB8TnDDQPg75zRAXUgTmaoRx",
		},
		{
			networkID:  LocalID,
			expectedID: "n8XH5JY1EX5VYqDeAhB4Zd4GKxi9UNQy6oPpMsCAj1Q6xkiiL",
		},
	}

	for _, test := range tests {
		_, avaxAssetID, err := Genesis(test.networkID)
		if err != nil {
			t.Fatal(err)
		}
		if result := avaxAssetID.String(); test.expectedID != result {
			t.Fatalf("AVA assetID with networkID %d was expected to be %s but was %s",
				test.networkID,
				test.expectedID,
				result)
		}
	}
}
