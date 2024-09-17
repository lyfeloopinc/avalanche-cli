// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package genesis

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ava-labs/avalanche-cli/pkg/application"
	"github.com/ava-labs/avalanche-cli/pkg/contract"
	"github.com/ava-labs/avalanche-cli/pkg/models"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	messengerVersion         = "0x1"
	MessengerContractAddress = "0x253b2784c75e510dD0fF1da844684a1aC0aa5fcf"
	RegistryContractAddress  = "0xF86Cb19Ad8405AEFa7d09C778215D2Cb6eBfB228"
	MessengerDeployerAddress = "0x618FEdD9A45a8C456812ecAAE70C671c6249DfaC"
)

//go:embed deployed_messenger_bytecode.txt
var deployedMessengerBytecode []byte

//go:embed deployed_registry_bytecode.txt
var deployedRegistryBytecode []byte

func setSimpleStorageValue(
	storage map[common.Hash]common.Hash,
	slot string,
	value string,
) {
	storage[common.HexToHash(slot)] = common.HexToHash(value)
}

func trimHexa(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
}

func hexFill32(s string) string {
	return fmt.Sprintf("%064s", trimHexa(s))
}

func setMappingStorageValue(
	storage map[common.Hash]common.Hash,
	slot string,
	key string,
	value string,
) error {
	slot = hexFill32(slot)
	key = hexFill32(key)
	storageKey := key + slot
	storageKeyBytes, err := hex.DecodeString(storageKey)
	if err != nil {
		return err
	}
	storage[crypto.Keccak256Hash(storageKeyBytes)] = common.HexToHash(value)
	return nil
}

func AddICMMessengerContractToAllocations(
	allocs core.GenesisAlloc,
) {
	const (
		blockchainIDSlot = "0x0"
		messageNonceSlot = "0x1"
	)
	storage := map[common.Hash]common.Hash{}
	setSimpleStorageValue(storage, blockchainIDSlot, "0x1")
	setSimpleStorageValue(storage, messageNonceSlot, "0x1")
	deployedMessengerBytes := common.FromHex(strings.TrimSpace(string(deployedMessengerBytecode)))
	allocs[common.HexToAddress(MessengerContractAddress)] = core.GenesisAccount{
		Balance: big.NewInt(0),
		Code:    deployedMessengerBytes,
		Storage: storage,
		Nonce:   1,
	}
	allocs[common.HexToAddress(MessengerDeployerAddress)] = core.GenesisAccount{
		Balance: big.NewInt(0),
		Nonce:   1,
	}
}

func AddICMRegistryContractToAllocations(
	allocs core.GenesisAlloc,
) error {
	const (
		latestVersionSlot    = "0x0"
		versionToAddressSlot = "0x1"
		addressToVersionSlot = "0x2"
	)
	storage := map[common.Hash]common.Hash{}
	setSimpleStorageValue(storage, latestVersionSlot, messengerVersion)
	if err := setMappingStorageValue(storage, versionToAddressSlot, messengerVersion, MessengerContractAddress); err != nil {
		return err
	}
	if err := setMappingStorageValue(storage, addressToVersionSlot, MessengerContractAddress, messengerVersion); err != nil {
		return err
	}
	deployedRegistryBytes := common.FromHex(strings.TrimSpace(string(deployedRegistryBytecode)))
	allocs[common.HexToAddress(RegistryContractAddress)] = core.GenesisAccount{
		Balance: big.NewInt(0),
		Code:    deployedRegistryBytes,
		Storage: storage,
		Nonce:   1,
	}
	return nil
}

func BlockchainHasICMEnabledGenesis(
	app *application.Avalanche,
	network models.Network,
	chainSpec contract.ChainSpec,
) (bool, error) {
	return contract.ContractAddressIsInBlockchainGenesis(app, network, chainSpec, common.HexToAddress(MessengerContractAddress))
}

func GenesisIsICMEnabled(
	genesisData []byte,
) (bool, error) {
	return contract.ContractAddressIsInGenesisData(genesisData, common.HexToAddress(MessengerContractAddress))
}
