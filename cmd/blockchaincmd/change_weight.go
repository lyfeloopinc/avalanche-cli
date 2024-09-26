// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package blockchaincmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/utils/formatting/address"
	warpPlatformVM "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/avalanche-cli/pkg/cobrautils"
	"github.com/ava-labs/avalanche-cli/pkg/constants"
	"github.com/ava-labs/avalanche-cli/pkg/keychain"
	"github.com/ava-labs/avalanche-cli/pkg/models"
	"github.com/ava-labs/avalanche-cli/pkg/networkoptions"
	"github.com/ava-labs/avalanche-cli/pkg/prompts"
	"github.com/ava-labs/avalanche-cli/pkg/subnet"
	"github.com/ava-labs/avalanche-cli/pkg/txutils"
	"github.com/ava-labs/avalanche-cli/pkg/utils"
	"github.com/ava-labs/avalanche-cli/pkg/ux"
	"github.com/ava-labs/avalanchego/ids"
	avagoconstants "github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/spf13/cobra"
)

var ()

// avalanche blockchain addValidator
func newChangeWeightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changeWeight [blockchainName] [nodeID]",
		Short: "Changes the weight of a Subnet validator",
		Long: `The blockchain changeWeight command changes the weight of a Subnet Validator.

The Subnet has to be a Proof of Authority Subnet-Only Validator Subnet.`,
		RunE: updateWeight,
		Args: cobrautils.ExactArgs(1),
	}
	networkoptions.AddNetworkFlagsToCmd(cmd, &globalNetworkFlags, true, addValidatorSupportedNetworkOptions)

	cmd.Flags().StringVarP(&keyName, "key", "k", "", "select the key to use [fuji/devnet only]")
	cmd.Flags().StringVar(&nodeIDStr, "nodeID", "", "set the NodeID of the validator to add")
	cmd.Flags().Uint64Var(&weight, "weight", constants.BootstrapValidatorWeight, "set the staking weight of the validator to add")
	cmd.Flags().BoolVarP(&useEwoq, "ewoq", "e", false, "use ewoq key [fuji/devnet only]")
	cmd.Flags().BoolVarP(&useLedger, "ledger", "g", false, "use ledger instead of key (always true on mainnet, defaults to false on fuji/devnet)")
	cmd.Flags().StringSliceVar(&ledgerAddresses, "ledger-addrs", []string{}, "use the given ledger addresses")
	cmd.Flags().BoolVar(&nonSOV, "not-sov", false, "set to true if adding validator to a non SOV blockchain")
	cmd.Flags().StringVar(&publicKey, "public-key", "", "set the BLS public key of the validator to add")
	cmd.Flags().StringVar(&pop, "proof-of-possession", "", "set the BLS proof of possession of the validator to add")
	return cmd
}

func updateWeight(_ *cobra.Command, args []string) error {
	blockchainName := args[0]
	nodeID := args[1]
	var err error

	network, err := networkoptions.GetNetworkFromCmdLineFlags(
		app,
		"",
		globalNetworkFlags,
		true,
		false,
		removeValidatorSupportedNetworkOptions,
		"",
	)
	if err != nil {
		return err
	}

	if outputTxPath != "" {
		if _, err := os.Stat(outputTxPath); err == nil {
			return fmt.Errorf("outputTxPath %q already exists", outputTxPath)
		}
	}

	if len(ledgerAddresses) > 0 {
		useLedger = true
	}

	if useLedger && keyName != "" {
		return ErrMutuallyExlusiveKeyLedger
	}
	
	switch network.Kind {
	case models.Local:
		return removeFromLocal(blockchainName)
	case models.Fuji:
		if !useLedger && keyName == "" {
			useLedger, keyName, err = prompts.GetKeyOrLedger(app.Prompt, constants.PayTxsFeesMsg, app.GetKeyDir(), false)
			if err != nil {
				return err
			}
		}
	case models.Mainnet:
		useLedger = true
		if keyName != "" {
			return ErrStoredKeyOnMainnet
		}
	default:
		return errors.New("unsupported network")
	}

	// get keychain accesor
	fee := network.GenesisParams().TxFeeConfig.StaticFeeConfig.TxFee
	kc, err := keychain.GetKeychain(app, false, useLedger, ledgerAddresses, keyName, network, fee)
	if err != nil {
		return err
	}

	network.HandlePublicNetworkSimulation()

	sc, err := app.LoadSidecar(blockchainName)
	if err != nil {
		return err
	}

	subnetID := sc.Networks[network.Name()].SubnetID
	if subnetID == ids.Empty {
		return errNoSubnetID
	}

	deployer := subnet.NewPublicDeployer(app, kc, network)
	if nonSOV {
		return removeValidatorNonSOV(deployer, network, subnetID, kc, blockchainName)
	}
	return removeValidatorSOV(deployer)
}

func promptValidatorBalance() (uint64, error) {
	ux.Logger.PrintToUser("Balance is used to pay for continuous fee to the P-Chain")
	txt := "What balance would you like to assign to the bootstrap validator (in AVAX)?"
	return app.Prompt.CaptureValidatorBalance(txt)
}

func CallAddValidator(
	deployer *subnet.PublicDeployer,
	network models.Network,
	kc *keychain.Keychain,
	useLedgerSetting bool,
	blockchainName string,
	nodeIDStr string,
	defaultValidatorParamsSetting bool,
) error {
	var (
		nodeID ids.NodeID
		err    error
	)

	useLedger = useLedgerSetting
	defaultValidatorParams = defaultValidatorParamsSetting

	_, err = ValidateSubnetNameAndGetChains([]string{blockchainName})
	if err != nil {
		return err
	}

	sc, err := app.LoadSidecar(blockchainName)
	if err != nil {
		return err
	}

	subnetID := sc.Networks[network.Name()].SubnetID
	if subnetID == ids.Empty {
		return errNoSubnetID
	}

	// TODO: implement getting validator manager controller address
	//kcKeys, err := kc.PChainFormattedStrAddresses()
	//if err != nil {
	//	return err
	//}

	if nodeIDStr == "" {
		nodeID, err = PromptNodeID("add as Subnet validator")
		if err != nil {
			return err
		}
	} else {
		nodeID, err = ids.NodeIDFromString(nodeIDStr)
		if err != nil {
			return err
		}
	}

	publicKey, pop, err = promptProofOfPossession(publicKey == "", pop == "")
	if err != nil {
		return err
	}

	balance, err := promptValidatorBalance()
	if err != nil {
		return err
	}

	changeAddr, err := getKeyForChangeOwner("", network)
	if err != nil {
		return err
	}

	ux.Logger.PrintToUser("NodeID: %s", nodeID.String())
	ux.Logger.PrintToUser("Network: %s", network.Name())
	ux.Logger.PrintToUser("Weight: %d", weight)
	ux.Logger.PrintToUser("Inputs complete, issuing transaction to add the provided validator information...")

	//type RegisterSubnetValidatorTx struct {
	//	// Metadata, inputs and outputs
	//	BaseTx
	//	// Balance <= sum($AVAX inputs) - sum($AVAX outputs) - TxFee.
	//	Balance uint64 `json:"balance"`
	//	// [Signer] is the BLS key for this validator.
	//	// Note: We do not enforce that the BLS key is unique across all validators.
	//	//       This means that validators can share a key if they so choose.
	//	//       However, a NodeID does uniquely map to a BLS key
	//	Signer signer.Signer `json:"signer"`
	//	// Leftover $AVAX from the Subnet Validator's Balance will be issued to
	//	// this owner after it is removed from the validator set.
	//	ChangeOwner fx.Owner `json:"changeOwner"`
	//	// AddressedCall with Payload:
	//	//   - SubnetID
	//	//   - NodeID (must be Ed25519 NodeID)
	//	//   - Weight
	//	//   - BLS public key
	//	//   - Expiry
	//	Message warp.Message `json:"message"`
	//}

	blsInfo, err := getBLSInfo(publicKey, pop)
	if err != nil {
		return fmt.Errorf("failure parsing BLS info: %w", err)
	}
	addrs, err := address.ParseToIDs([]string{changeAddr})
	if err != nil {
		return fmt.Errorf("failure parsing change owner address: %w", err)
	}
	changeOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     addrs,
	}
	// TODO: generate warp message
	message, err := generateWarpMessageAddValidator()
	tx, err := deployer.RegisterSubnetValidator(balance, blsInfo, changeOwner, message)
	if err != nil {
		return err
	}
	ux.Logger.GreenCheckmarkToUser("Register Subnet Validator Tx ID: %s", tx.ID())
	return nil
}

func generateWarpMessageAddValidator() (warpPlatformVM.Message, error) {
	return warpPlatformVM.Message{}, nil
}

func CallAddValidatorNonSOV(
	deployer *subnet.PublicDeployer,
	network models.Network,
	kc *keychain.Keychain,
	useLedgerSetting bool,
	blockchainName string,
	nodeIDStr string,
	defaultValidatorParamsSetting bool,
	waitForTxAcceptanceSetting bool,
) error {
	var (
		nodeID ids.NodeID
		start  time.Time
		err    error
	)

	useLedger = useLedgerSetting
	defaultValidatorParams = defaultValidatorParamsSetting
	waitForTxAcceptance = waitForTxAcceptanceSetting

	if defaultValidatorParams {
		useDefaultDuration = true
		useDefaultStartTime = true
		useDefaultWeight = true
	}

	if useDefaultDuration && duration != 0 {
		return errMutuallyExclusiveDurationOptions
	}
	if useDefaultStartTime && startTimeStr != "" {
		return errMutuallyExclusiveStartOptions
	}
	if useDefaultWeight && weight != 0 {
		return errMutuallyExclusiveWeightOptions
	}

	if outputTxPath != "" {
		if utils.FileExists(outputTxPath) {
			return fmt.Errorf("outputTxPath %q already exists", outputTxPath)
		}
	}

	_, err = ValidateSubnetNameAndGetChains([]string{blockchainName})
	if err != nil {
		return err
	}

	sc, err := app.LoadSidecar(blockchainName)
	if err != nil {
		return err
	}

	subnetID := sc.Networks[network.Name()].SubnetID
	if subnetID == ids.Empty {
		return errNoSubnetID
	}

	isPermissioned, controlKeys, threshold, err := txutils.GetOwners(network, subnetID)
	if err != nil {
		return err
	}
	if !isPermissioned {
		return ErrNotPermissionedSubnet
	}

	kcKeys, err := kc.PChainFormattedStrAddresses()
	if err != nil {
		return err
	}

	// get keys for add validator tx signing
	if subnetAuthKeys != nil {
		if err := prompts.CheckSubnetAuthKeys(kcKeys, subnetAuthKeys, controlKeys, threshold); err != nil {
			return err
		}
	} else {
		subnetAuthKeys, err = prompts.GetSubnetAuthKeys(app.Prompt, kcKeys, controlKeys, threshold)
		if err != nil {
			return err
		}
	}
	ux.Logger.PrintToUser("Your subnet auth keys for add validator tx creation: %s", subnetAuthKeys)

	if nodeIDStr == "" {
		nodeID, err = PromptNodeID("add as validator")
		if err != nil {
			return err
		}
	} else {
		nodeID, err = ids.NodeIDFromString(nodeIDStr)
		if err != nil {
			return err
		}
	}

	selectedWeight, err := getWeight()
	if err != nil {
		return err
	}
	if selectedWeight < constants.MinStakeWeight {
		return fmt.Errorf("invalid weight, must be greater than or equal to %d: %d", constants.MinStakeWeight, selectedWeight)
	}

	start, selectedDuration, err := getTimeParameters(network, nodeID, true)
	if err != nil {
		return err
	}

	ux.Logger.PrintToUser("NodeID: %s", nodeID.String())
	ux.Logger.PrintToUser("Network: %s", network.Name())
	ux.Logger.PrintToUser("Start time: %s", start.Format(constants.TimeParseLayout))
	ux.Logger.PrintToUser("End time: %s", start.Add(selectedDuration).Format(constants.TimeParseLayout))
	ux.Logger.PrintToUser("Weight: %d", selectedWeight)
	ux.Logger.PrintToUser("Inputs complete, issuing transaction to add the provided validator information...")

	isFullySigned, tx, remainingSubnetAuthKeys, err := deployer.AddValidatorNonSOV(
		waitForTxAcceptance,
		controlKeys,
		subnetAuthKeys,
		subnetID,
		nodeID,
		selectedWeight,
		start,
		selectedDuration,
	)
	if err != nil {
		return err
	}
	if !isFullySigned {
		if err := SaveNotFullySignedTx(
			"Add Validator",
			tx,
			blockchainName,
			subnetAuthKeys,
			remainingSubnetAuthKeys,
			outputTxPath,
			false,
		); err != nil {
			return err
		}
	}

	return err
}

func PromptDuration(start time.Time, network models.Network) (time.Duration, error) {
	for {
		txt := "How long should this validator be validating? Enter a duration, e.g. 8760h. Valid time units are \"ns\", \"us\" (or \"µs\"), \"ms\", \"s\", \"m\", \"h\""
		var d time.Duration
		var err error
		if network.Kind == models.Fuji {
			d, err = app.Prompt.CaptureFujiDuration(txt)
		} else {
			d, err = app.Prompt.CaptureMainnetDuration(txt)
		}
		if err != nil {
			return 0, err
		}
		end := start.Add(d)
		confirm := fmt.Sprintf("Your validator will finish staking by %s", end.Format(constants.TimeParseLayout))
		yes, err := app.Prompt.CaptureYesNo(confirm)
		if err != nil {
			return 0, err
		}
		if yes {
			return d, nil
		}
	}
}

func getMaxValidationTime(network models.Network, nodeID ids.NodeID, startTime time.Time) (time.Duration, error) {
	ctx, cancel := utils.GetAPIContext()
	defer cancel()
	platformCli := platformvm.NewClient(network.Endpoint)
	vs, err := platformCli.GetCurrentValidators(ctx, avagoconstants.PrimaryNetworkID, nil)
	cancel()
	if err != nil {
		return 0, err
	}
	for _, v := range vs {
		if v.NodeID == nodeID {
			return time.Unix(int64(v.EndTime), 0).Sub(startTime), nil
		}
	}
	return 0, errors.New("nodeID not found in validator set: " + nodeID.String())
}

func getTimeParameters(network models.Network, nodeID ids.NodeID, isValidator bool) (time.Time, time.Duration, error) {
	defaultStakingStartLeadTime := constants.StakingStartLeadTime
	if network.Kind == models.Devnet {
		defaultStakingStartLeadTime = constants.DevnetStakingStartLeadTime
	}

	const custom = "Custom"

	// this sets either the global var startTimeStr or useDefaultStartTime to enable repeated execution with
	// state keeping from node cmds
	if startTimeStr == "" && !useDefaultStartTime {
		if isValidator {
			ux.Logger.PrintToUser("When should your validator start validating?\n" +
				"If you validator is not ready by this time, subnet downtime can occur.")
		} else {
			ux.Logger.PrintToUser("When do you want to start delegating?\n")
		}
		defaultStartOption := "Start in " + ux.FormatDuration(defaultStakingStartLeadTime)
		startTimeOptions := []string{defaultStartOption, custom}
		startTimeOption, err := app.Prompt.CaptureList("Start time", startTimeOptions)
		if err != nil {
			return time.Time{}, 0, err
		}
		switch startTimeOption {
		case defaultStartOption:
			useDefaultStartTime = true
		default:
			start, err := promptStart()
			if err != nil {
				return time.Time{}, 0, err
			}
			startTimeStr = start.Format(constants.TimeParseLayout)
		}
	}

	var (
		err   error
		start time.Time
	)
	if startTimeStr != "" {
		start, err = time.Parse(constants.TimeParseLayout, startTimeStr)
		if err != nil {
			return time.Time{}, 0, err
		}
		if start.Before(time.Now().Add(constants.StakingMinimumLeadTime)) {
			return time.Time{}, 0, fmt.Errorf("time should be at least %s in the future ", constants.StakingMinimumLeadTime)
		}
	} else {
		start = time.Now().Add(defaultStakingStartLeadTime)
	}

	// this sets either the global var duration or useDefaultDuration to enable repeated execution with
	// state keeping from node cmds
	if duration == 0 && !useDefaultDuration {
		msg := "How long should your validator validate for?"
		if !isValidator {
			msg = "How long do you want to delegate for?"
		}
		const defaultDurationOption = "Until primary network validator expires"
		durationOptions := []string{defaultDurationOption, custom}
		durationOption, err := app.Prompt.CaptureList(msg, durationOptions)
		if err != nil {
			return time.Time{}, 0, err
		}
		switch durationOption {
		case defaultDurationOption:
			useDefaultDuration = true
		default:
			duration, err = PromptDuration(start, network)
			if err != nil {
				return time.Time{}, 0, err
			}
		}
	}

	var selectedDuration time.Duration
	if useDefaultDuration {
		// avoid setting both globals useDefaultDuration and duration
		selectedDuration, err = getMaxValidationTime(network, nodeID, start)
		if err != nil {
			return time.Time{}, 0, err
		}
	} else {
		selectedDuration = duration
	}

	return start, selectedDuration, nil
}

func promptStart() (time.Time, error) {
	txt := "When should the validator start validating? Enter a UTC datetime in 'YYYY-MM-DD HH:MM:SS' format"
	return app.Prompt.CaptureDate(txt)
}

func PromptNodeID(goal string) (ids.NodeID, error) {
	txt := fmt.Sprintf("What is the NodeID of the node you want to %s?", goal)
	return app.Prompt.CaptureNodeID(txt)
}

func getWeight() (uint64, error) {
	// this sets either the global var weight or useDefaultWeight to enable repeated execution with
	// state keeping from node cmds
	if weight == 0 && !useDefaultWeight {
		defaultWeight := fmt.Sprintf("Default (%d)", constants.DefaultStakeWeight)
		txt := "What stake weight would you like to assign to the validator?"
		weightOptions := []string{defaultWeight, "Custom"}
		weightOption, err := app.Prompt.CaptureList(txt, weightOptions)
		if err != nil {
			return 0, err
		}
		switch weightOption {
		case defaultWeight:
			useDefaultWeight = true
		default:
			weight, err = app.Prompt.CaptureWeight(txt)
			if err != nil {
				return 0, err
			}
		}
	}
	if useDefaultWeight {
		return constants.DefaultStakeWeight, nil
	}
	return weight, nil
}
