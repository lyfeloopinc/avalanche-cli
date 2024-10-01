// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"time"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const numTriggerTxs = 5 // Number of txs needed to activate the proposer VM fork

// IssueTxsToActivateProposerVMFork issues transactions at the current
// timestamp, which should be after the ProposerVM activation time (aka
// ApricotPhase4). This should generate a PostForkBlock because its parent block
// (genesis) has a timestamp (0) that is greater than or equal to the fork
// activation time of 0. Therefore, subsequent blocks should be built with
// BuildBlockWithContext.
func SubnetEVMIssueTxsToActivateProposerVMFork(
	ctx context.Context, chainID *big.Int, fundedKey *ecdsa.PrivateKey,
	client ethclient.Client,
) error {
	addr := crypto.PubkeyToAddress(fundedKey.PublicKey)
	nonce, err := client.NonceAt(ctx, addr, nil)
	if err != nil {
		return err
	}

	gasPrice := big.NewInt(params.MinGasPrice)
	txSigner := types.LatestSignerForChainID(chainID)
	for i := 0; i < numTriggerTxs; i++ {
		tx := types.NewTransaction(
			nonce, addr, common.Big1, params.TxGas, gasPrice, nil)
		triggerTx, err := types.SignTx(tx, txSigner, fundedKey)
		if err != nil {
			return err
		}
		if err := client.SendTransaction(ctx, triggerTx); err != nil {
			return err
		}
		time.Sleep(5 * time.Second)
		nonce++
	}
	log.Info(
		"Built sufficient blocks to activate proposerVM fork",
		"txCount", numTriggerTxs,
	)
	return nil
}
