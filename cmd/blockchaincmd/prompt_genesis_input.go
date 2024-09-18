// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package blockchaincmd

import (
	"fmt"
	"github.com/ava-labs/avalanche-cli/pkg/application"
	"github.com/ava-labs/avalanche-cli/pkg/constants"
	"github.com/ava-labs/avalanche-cli/pkg/models"
	"github.com/ava-labs/avalanche-cli/pkg/prompts"
	"github.com/ava-labs/avalanche-cli/pkg/utils"
	"github.com/ava-labs/avalanche-cli/pkg/ux"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

func getValidatorContractManagerAddr() ([]string, bool, error) {
	controllerAddrPrompt := "Enter Validator Manager Contract controller address"
	for {
		// ask in a loop so that if some condition is not met we can keep asking
		controlAddr, cancelled, err := getAddrLoop(controllerAddrPrompt, constants.ValidatorManagerController, models.UndefinedNetwork)
		if err != nil {
			return nil, false, err
		}
		if cancelled {
			return nil, cancelled, nil
		}
		if len(controlAddr) != 0 {
			return controlAddr, false, nil
		}
		ux.Logger.RedXToUser("An address to control Validator Manage Contract is required before proceeding")
	}
}

// Configure which addresses may make mint new native tokens
func getTokenMinterAddr() ([]string, error) {
	addTokenMinterAddrPrompt := "Currently only Validator Manager Contract can mint new native tokens"
	ux.Logger.PrintToUser(addTokenMinterAddrPrompt)
	yes, err := app.Prompt.CaptureNoYes("Add additional addresses that can mint new native tokens?")
	if err != nil {
		return nil, err
	}
	if !yes {
		return nil, nil
	}
	addr, cancelled, err := getAddr()
	if err != nil {
		return nil, err
	}
	if cancelled {
		return nil, nil
	}
	return addr, nil
}

func getAddr() ([]string, bool, error) {
	addrPrompt := "Enter addresses that can mint new native tokens"
	addr, cancelled, err := getAddrLoop(addrPrompt, constants.TokenMinter, models.UndefinedNetwork)
	if err != nil {
		return nil, false, err
	}
	if cancelled {
		return nil, cancelled, nil
	}
	return addr, false, nil
}

func promptProofOfPossession() (string, string, error) {
	ux.Logger.PrintToUser("Next, we need the public key and proof of possession of the node's BLS")
	ux.Logger.PrintToUser("Check https://docs.avax.network/api-reference/info-api#infogetnodeid for instructions on calling info.getNodeID API")
	var err error
	txt := "What is the public key of the node's BLS?"
	publicKey, err := app.Prompt.CaptureValidatedString(txt, prompts.ValidateHexa)
	if err != nil {
		return "", "", err
	}
	txt = "What is the proof of possession of the node's BLS?"
	proofOfPossesion, err := app.Prompt.CaptureValidatedString(txt, prompts.ValidateHexa)
	if err != nil {
		return "", "", err
	}
	return publicKey, proofOfPossesion, nil
}

// TODO: add explain the difference for different validator management type
func promptValidatorManagementType(
	app *application.Avalanche,
	sidecar *models.Sidecar,
) error {
	proofOfAuthorityOption := "Proof of Authority"
	proofOfStakeOption := "Proof of Stake"
	explainOption := "Explain the difference"
	if createFlags.proofOfStake {
		sidecar.ValidatorManagement = models.ValidatorManagementTypeFromString(proofOfStakeOption)
		return nil
	}
	if createFlags.proofOfAuthority {
		sidecar.ValidatorManagement = models.ValidatorManagementTypeFromString(proofOfAuthorityOption)
		return nil
	}
	options := []string{proofOfAuthorityOption, proofOfStakeOption, explainOption}
	var subnetTypeStr string
	for {
		option, err := app.Prompt.CaptureList(
			"Which validator management protocol would you like to use in your blockchain?",
			options,
		)
		if err != nil {
			return err
		}
		switch option {
		case proofOfAuthorityOption:
			subnetTypeStr = models.ProofOfAuthority
		case proofOfStakeOption:
			subnetTypeStr = models.ProofOfStake
		case explainOption:
			continue
		}
		break
	}
	sidecar.ValidatorManagement = models.ValidatorManagementTypeFromString(subnetTypeStr)
	return nil
}

func promptBootstrapValidators() ([]models.SubnetValidator, error) {
	var subnetValidators []models.SubnetValidator
	numBootstrapValidators, err := app.Prompt.CaptureInt(
		"How many bootstrap validators do you want to set up?",
	)
	if err != nil {
		return nil, err
	}
	setUpNodes, err := promptSetUpNodes()
	if err != nil {
		return nil, err
	}
	previousAddr := ""
	for len(subnetValidators) < numBootstrapValidators {
		ux.Logger.PrintToUser("Getting info for bootstrap validator %d", len(subnetValidators)+1)
		var nodeID ids.NodeID
		var publicKey, pop string
		if setUpNodes {
			nodeID, err = PromptNodeID()
			if err != nil {
				return nil, err
			}
			publicKey, pop, err = promptProofOfPossession()
			if err != nil {
				return nil, err
			}
		} else {
			certBytes, _, err := staking.NewCertAndKeyBytes()
			if err != nil {
				return nil, err
			}
			nodeID, err = utils.ToNodeID(certBytes)
			if err != nil {
				return nil, err
			}
			blsSignerKey, err := bls.NewSecretKey()
			if err != nil {
				return nil, err
			}
			p := signer.NewProofOfPossession(blsSignerKey)
			publicKey, err = formatting.Encode(formatting.HexNC, p.PublicKey[:])
			if err != nil {
				return nil, err
			}
			pop, err = formatting.Encode(formatting.HexNC, p.ProofOfPossession[:])
			if err != nil {
				return nil, err
			}
		}
		changeAddr, err := getKeyForChangeOwner(previousAddr)
		if err != nil {
			return nil, err
		}
		previousAddr = changeAddr
		subnetValidator := models.SubnetValidator{
			NodeID:               nodeID.String(),
			Weight:               constants.DefaultWeightBootstrapValidator,
			Balance:              constants.InitialBalanceBootstrapValidator,
			BLSPublicKey:         publicKey,
			BLSProofOfPossession: pop,
			ChangeOwnerAddr:      changeAddr,
		}
		subnetValidators = append(subnetValidators, subnetValidator)
		ux.Logger.GreenCheckmarkToUser("Bootstrap Validator %d:", len(subnetValidators))
		ux.Logger.PrintToUser("- Node ID: %s", nodeID)
		ux.Logger.PrintToUser("- Change Address: %s", changeAddr)
	}
	return subnetValidators, nil
}

func validateBLS(publicKey, pop string) error {
	if err := prompts.ValidateHexa(publicKey); err != nil {
		return fmt.Errorf("format error in given public key: %s", err)
	}
	if err := prompts.ValidateHexa(pop); err != nil {
		return fmt.Errorf("format error in given proof of possession: %s", err)
	}
	return nil
}

func validateSubnetValidatorsJSON(validatorJSONS []models.SubnetValidator) error {
	for _, validatorJSON := range validatorJSONS {
		_, err := ids.NodeIDFromString(validatorJSON.NodeID)
		if err != nil {
			return fmt.Errorf("invalid node id %s", validatorJSON.NodeID)
		}
		if validatorJSON.Weight <= 0 {
			return fmt.Errorf("bootstrap validator weight has to be greater than 0")
		}
		if validatorJSON.Balance <= 0 {
			return fmt.Errorf("bootstrap validator balance has to be greater than 0")
		}
		if err = validateBLS(validatorJSON.BLSPublicKey, validatorJSON.BLSProofOfPossession); err != nil {
			return err
		}
	}
	return nil
}

// promptProvideNodeID returns false if user doesn't have any Avalanche node set up yet to be
// bootstrap validators
func promptSetUpNodes() (bool, error) {
	ux.Logger.PrintToUser("If you have set up your own Avalanche Nodes, you can provide the Node ID and BLS Key from those nodes in the next step.")
	ux.Logger.PrintToUser("Otherwise, we will generate new Node IDs and BLS Key for you.")
	setUpNodes, err := app.Prompt.CaptureYesNo("Have you set up your own Avalanche Nodes?")
	if err != nil {
		return false, err
	}
	return setUpNodes, nil
}
