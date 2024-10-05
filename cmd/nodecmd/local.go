// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package nodecmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ava-labs/avalanche-cli/pkg/binutils"
	"github.com/ava-labs/avalanche-cli/pkg/cobrautils"
	"github.com/ava-labs/avalanche-cli/pkg/constants"
	"github.com/ava-labs/avalanche-cli/pkg/models"
	"github.com/ava-labs/avalanche-cli/pkg/networkoptions"
	"github.com/ava-labs/avalanche-cli/pkg/subnet"
	"github.com/ava-labs/avalanche-cli/pkg/utils"
	"github.com/ava-labs/avalanche-cli/pkg/ux"
	"github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/avalanche-network-runner/server"
	"github.com/spf13/cobra"
)

var (
	avalanchegoBinaryPath string

	bootstrapIDs  []string
	bootstrapIPs  []string
	genesisPath   string
	upgradePath   string
	useEtnaDevnet bool
)

// const snapshotName = "local_snapshot"
func newLocalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "local",
		Short: "(ALPHA Warning) Suite of commands for a local avalanche node",
		Long: `(ALPHA Warning) This command is currently in experimental mode.

The node local command suite provides a collection of commands related to local nodes`,
		RunE: cobrautils.CommandSuiteUsage,
	}
	// node local start
	cmd.AddCommand(newLocalStartCmd())
	// node local stop
	cmd.AddCommand(newLocalStopCmd())
	// node local destroy
	cmd.AddCommand(newLocalDestroyCmd())
	return cmd
}

func newLocalStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [clusterName]",
		Short: "(ALPHA Warning) Create a new validator on local machine",
		Long: `(ALPHA Warning) This command is currently in experimental mode. 

The node local start command sets up a validator on a local server. 
The validator will be validating the Avalanche Primary Network and Subnet 
of your choice. By default, the command runs an interactive wizard. It 
walks you through all the steps you need to set up a validator.
Once this command is completed, you will have to wait for the validator
to finish bootstrapping on the primary network before running further
commands on it, e.g. validating a Subnet. You can check the bootstrapping
status by running avalanche node status local 
`,
		Args:              cobra.ExactArgs(1),
		RunE:              localStartNode,
		PersistentPostRun: handlePostRun,
	}
	networkoptions.AddNetworkFlagsToCmd(cmd, &globalNetworkFlags, false, createSupportedNetworkOptions)
	cmd.Flags().BoolVar(&useLatestAvalanchegoReleaseVersion, "latest-avalanchego-version", false, "install latest avalanchego release version on node/s")
	cmd.Flags().BoolVar(&useLatestAvalanchegoPreReleaseVersion, "latest-avalanchego-pre-release-version", false, "install latest avalanchego pre-release version on node/s")
	cmd.Flags().StringVar(&useCustomAvalanchegoVersion, "custom-avalanchego-version", "", "install given avalanchego version on node/s")
	cmd.Flags().StringVar(&avalanchegoBinaryPath, "avalanchego-path", "", "use this avalanchego binary path")
	cmd.Flags().StringArrayVar(&bootstrapIDs, "bootstrap-id", []string{}, "nodeIDs of bootstrap nodes")
	cmd.Flags().StringArrayVar(&bootstrapIPs, "bootstrap-ip", []string{}, "IP:port pairs of bootstrap nodes")
	cmd.Flags().StringVar(&genesisPath, "genesis", "", "path to genesis file")
	cmd.Flags().StringVar(&upgradePath, "upgrade", "", "path to upgrade file")
	cmd.Flags().BoolVar(&useEtnaDevnet, "etna-devnet", false, "use Etna devnet. Prepopulated with Etna DevNet bootstrap configuration along with genesis and upgrade files")
	return cmd
}

func newLocalStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [clusterName]",
		Short: "(ALPHA Warning) Stop local node",
		Long:  `Stop local node.`,
		Args:  cobra.ExactArgs(1),
		RunE:  localStopNode,
	}
}

func newLocalDestroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "destroy [clusterName]",
		Short: "(ALPHA Warning) Cleanup local node",
		Long:  `Cleanup local node.`,
		Args:  cobra.ExactArgs(1),
		RunE:  localDestroyNode,
	}
}

// stub for now
func preLocalChecks(clusterName string) error {
	clusterExists, err := checkClusterExists(clusterName)
	if err != nil {
		return fmt.Errorf("error checking cluster: %w", err)
	}
	if clusterExists {
		return fmt.Errorf("cluster %q already exists", clusterName)
	}
	// expand passed paths
	if genesisPath != "" {
		genesisPath = utils.ExpandHome(genesisPath)
	}
	if upgradePath != "" {
		upgradePath = utils.ExpandHome(upgradePath)
	}
	// checks
	if useCustomAvalanchegoVersion != "" && (useLatestAvalanchegoReleaseVersion || useLatestAvalanchegoPreReleaseVersion) {
		return fmt.Errorf("specify either --custom-avalanchego-version or --latest-avalanchego-version")
	}
	if avalanchegoBinaryPath != "" && (useLatestAvalanchegoReleaseVersion || useLatestAvalanchegoPreReleaseVersion || useCustomAvalanchegoVersion != "") {
		return fmt.Errorf("specify either --avalanchego-path or --latest-avalanchego-version or --custom-avalanchego-version")
	}
	if useEtnaDevnet && (globalNetworkFlags.UseDevnet || globalNetworkFlags.UseFuji) {
		return fmt.Errorf("etna devnet can only be used with devnet")
	}
	if useEtnaDevnet && genesisPath != "" {
		return fmt.Errorf("etna devnet uses predefined genesis file")
	}
	if useEtnaDevnet && upgradePath != "" {
		return fmt.Errorf("etna devnet uses predefined upgrade file")
	}
	if useEtnaDevnet && (len(bootstrapIDs) != 0 || len(bootstrapIPs) != 0) {
		return fmt.Errorf("etna devnet uses predefined bootstrap configuration")
	}
	if len(bootstrapIDs) != len(bootstrapIPs) {
		return fmt.Errorf("number of bootstrap IDs and bootstrap IP:port pairs must be equal")
	}
	if genesisPath != "" && !utils.FileExists(genesisPath) {
		return fmt.Errorf("genesis file %s does not exist", genesisPath)
	}
	if upgradePath != "" && !utils.FileExists(upgradePath) {
		return fmt.Errorf("upgrade file %s does not exist", upgradePath)
	}
	return nil
}

func localClusterDataExists(clusterName string) bool {
	return utils.FileExists(filepath.Join(app.GetLocalDir(clusterName), "state.json"))
}

func localClusterSnapshotName(clusterName string) string {
	return fmt.Sprintf("local-snapshot-%s", clusterName)
}

func localStartNode(_ *cobra.Command, args []string) error {
	var err error
	clusterName := args[0]
	network := models.UndefinedNetwork
	networkID := uint32(0)

	// check if this is existing cluster
	rootDir := app.GetLocalDir(clusterName)
	logDir := filepath.Join(rootDir, "logs")
	pluginDir := app.GetPluginsDir()

	cli, err := binutils.NewGRPCClient()
	if err != nil {
		return err
	}

	ctx, cancel := utils.GetANRContext()
	defer cancel()

	if localClusterDataExists(clusterName) {
		ux.Logger.GreenCheckmarkToUser("Local cluster %s found. Booting up...", clusterName)
	} else {
		ux.Logger.GreenCheckmarkToUser("Local cluster %s not found. Creating...", clusterName)
		if useEtnaDevnet {
			networkID = constants.EtnaDevnetNetworkID
		} else {
			network, err = networkoptions.GetNetworkFromCmdLineFlags(
				app,
				"",
				globalNetworkFlags,
				false,
				true,
				createSupportedNetworkOptions,
				"",
			)
			if err != nil {
				return err
			}
			networkID = network.ID
		}
		avalancheGoVersion := "latest"
		if avalanchegoBinaryPath == "" {
			avalancheGoVersion, err = getAvalancheGoVersion()
			if err != nil {
				return err
			} else {
				ux.Logger.PrintToUser("Using AvalancheGo version: %s", avalancheGoVersion)
			}
		}
		if err := preLocalChecks(clusterName); err != nil {
			return err
		}
		if useEtnaDevnet {
			bootstrapIDs = constants.EtnaDevnetBootstrapNodeIDs
			bootstrapIPs = constants.EtnaDevnetBootstrapIPs
			// prepare genesis and upgrade files for anr
			genesisFile, err := os.CreateTemp("", "etna_devnet_genesis")
			if err != nil {
				return fmt.Errorf("could not create save Etna Devnet genesis file: %w", err)
			}
			if _, err := genesisFile.Write(constants.EtnaDevnetGenesisData); err != nil {
				return fmt.Errorf("could not write Etna Devnet genesis data: %w", err)
			}
			if err := genesisFile.Close(); err != nil {
				return fmt.Errorf("could not close Etna Devnet genesis file: %w", err)
			}
			genesisPath = genesisFile.Name()
			defer os.Remove(genesisPath)

			upgradeFile, err := os.CreateTemp("", "etna_devnet_upgrade")
			if err != nil {
				return fmt.Errorf("could not create save Etna Devnet upgrade file: %w", err)
			}
			if _, err := upgradeFile.Write(constants.EtnaDevnetUpgradeData); err != nil {
				return fmt.Errorf("could not write Etna Devnet upgrade data: %w", err)
			}
			upgradePath = upgradeFile.Name()
			if err := upgradeFile.Close(); err != nil {
				return fmt.Errorf("could not close Etna Devnet upgrade file: %w", err)
			}
			defer os.Remove(upgradePath)
		}

		sd := subnet.NewLocalDeployer(app, avalancheGoVersion, avalanchegoBinaryPath, "")

		if err := sd.StartServer(); err != nil {
			return err
		}

		_, avalancheGoBinPath, err := sd.SetupLocalEnv()
		if err != nil {
			return err
		}

		// make sure rootDir exists
		if err := os.MkdirAll(rootDir, 0o700); err != nil {
			return fmt.Errorf("could not create root directory %s: %w", rootDir, err)
		}
		if err := os.MkdirAll(logDir, 0o700); err != nil {
			return fmt.Errorf("could not create log directory %s: %w", logDir, err)
		}

		ux.Logger.PrintToUser("Starting local avalanchego node using root: %s ...", rootDir)

		anrOpts := []client.OpOption{
			client.WithNumNodes(1),
			client.WithNetworkID(networkID),
			client.WithExecPath(avalancheGoBinPath),
			client.WithRootDataDir(rootDir),
			client.WithLogRootDir(logDir),
			client.WithReassignPortsIfUsed(true),
			client.WithPluginDir(pluginDir),
		}
		// load global node configs if they exist
		configStr, err := app.Conf.LoadNodeConfig()
		if err != nil {
			return err
		}
		if configStr != "" {
			anrOpts = append(anrOpts, client.WithGlobalNodeConfig(configStr))
		}
		if genesisPath != "" && utils.FileExists(genesisPath) {
			anrOpts = append(anrOpts, client.WithGenesisPath(genesisPath))
		}
		if upgradePath != "" && utils.FileExists(upgradePath) {
			anrOpts = append(anrOpts, client.WithUpgradePath(upgradePath))
		}
		if bootstrapIDs != nil {
			anrOpts = append(anrOpts, client.WithBootstrapNodeIDs(bootstrapIDs))
		}
		if bootstrapIPs != nil {
			anrOpts = append(anrOpts, client.WithBootstrapNodeIPPortPairs(bootstrapIPs))
		}

		ux.Logger.PrintToUser("Booting Network. Wait until healthy...")

		startResp, err := cli.Start(ctx, avalancheGoBinPath, anrOpts...)
		if err != nil {
			return fmt.Errorf("failed to start local avalanchego: %w \n %s", err, startResp)
		}
		// save snapshot after successful start
		saveSnapshotResponse, err := cli.SaveSnapshot(
			ctx,
			localClusterSnapshotName(clusterName),
			true, // force
		)
		if err != nil {
			return fmt.Errorf("failed to save snapshot: %w \n %s", err, saveSnapshotResponse)
		}
	}

	loadSnapshotOpts := []client.OpOption{
		client.WithRootDataDir(rootDir),
		client.WithLogRootDir(logDir),
		client.WithReassignPortsIfUsed(true),
		client.WithPluginDir(pluginDir),
	}
	// load snapshot for existing network
	loadSnapshotResponse, err := cli.LoadSnapshot(
		ctx,
		localClusterSnapshotName(clusterName),
		false, // in-place
		loadSnapshotOpts...)
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %w \n %s", err, loadSnapshotResponse)
	}

	ux.Logger.GreenCheckmarkToUser("Avalanchego started and ready to use from %s", rootDir)
	ux.Logger.PrintToUser("")
	ux.Logger.PrintToUser("Node logs directory: %s/node1/logs", logDir)
	ux.Logger.PrintToUser("")
	ux.Logger.PrintToUser("Network ready to use.")
	ux.Logger.PrintToUser("")

	return nil
}

func localStopNode(_ *cobra.Command, _ []string) error {
	cli, err := binutils.NewGRPCClient(
		binutils.WithAvoidRPCVersionCheck(true),
		binutils.WithDialTimeout(constants.FastGRPCDialTimeout),
	)
	if err != nil {
		return err
	}

	ctx, cancel := utils.GetANRContext()
	defer cancel()

	if _, err := cli.Status(ctx); err != nil {
		if server.IsServerError(err, server.ErrNotBootstrapped) {
			ux.Logger.PrintToUser("avalanchego already stopped.")
			return nil
		}
		return fmt.Errorf("failed to get avalanchego status: %w", err)
	}
	if _, err = cli.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop avalanchego: %w", err)
	}

	return nil
}

func localDestroyNode(_ *cobra.Command, args []string) error {
	clusterName := args[0]
	/*if err := checkCluster(clusterName); err != nil {
		return err
	}
	*/
	if err := cleanupLocalNode(clusterName); err != nil {
		return fmt.Errorf("failed to cleanup local node: %w", err)
	}
	ux.Logger.PrintToUser("Local node cleaned up.")
	return nil
}

func cleanupLocalNode(clusterName string) error {
	rootDir := app.GetLocalDir(clusterName)
	return os.RemoveAll(rootDir)
}
