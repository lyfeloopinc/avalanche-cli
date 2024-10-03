// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package updatecmd

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanche-cli/pkg/application"
	"github.com/ava-labs/avalanche-cli/pkg/binutils"
	"github.com/ava-labs/avalanche-cli/pkg/cobrautils"
	"github.com/ava-labs/avalanche-cli/pkg/constants"
	"github.com/ava-labs/avalanche-cli/pkg/ux"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/mod/semver"
)

var (
	ErrUserAbortedInstallation = errors.New("user canceled installation")
	ErrNoVersion               = errors.New("failed to find current version - did you install following official instructions?")
	app                        *application.Avalanche
	yes                        bool
)

func NewCmd(injectedApp *application.Avalanche, version string) *cobra.Command {
	app = injectedApp
	cmd := &cobra.Command{
		Use:     "update",
		Short:   "Check for latest updates of Avalanche-CLI",
		Long:    `Check if an update is available, and prompt the user to install it`,
		RunE:    runUpdate,
		Args:    cobrautils.ExactArgs(0),
		Version: version,
	}

	cmd.Flags().BoolVarP(&yes, "confirm", "c", false, "Assume yes for installation")
	return cmd
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	isUserCalled := true
	return Update(cmd, isUserCalled, "", &application.LastActions{})
}

func Update(cmd *cobra.Command, isUserCalled bool, version string, lastActs *application.LastActions) error {
	// first check if there is a new version exists
	url := binutils.GetGithubLatestReleaseURL(constants.AvaLabsOrg, constants.CliRepoName)
	latest, err := app.Downloader.GetLatestReleaseVersion(url)
	if err != nil {
		app.Log.Warn("failed to get latest version for cli from repo", zap.Error(err))
		return err
	}

	if lastActs == nil {
		lastActs = &application.LastActions{}
	}
	lastActs.LastCheckGit = time.Now()
	app.WriteLastActionsFile(lastActs)

	// the current version info should be in this variable
	this := cmd.Version
	if this == "" {
		if version != "" {
			this = version
		} else {
			// try loading from file system
			verFile := "VERSION"
			bver, err := os.ReadFile(verFile)
			if err != nil {
				app.Log.Warn("failed to read version from file on disk", zap.Error(err))
				return ErrNoVersion
			}
			this = strings.TrimSpace(string(bver))
		}
	}
	thisVFmt := "v" + this

	// check this version needs update
	// we skip if compare returns -1 (latest < this)
	// or 0 (latest == this)
	if semver.Compare(latest, thisVFmt) < 1 {
		txt := "No new version found upstream; skipping update"
		app.Log.Debug(txt)
		if isUserCalled {
			ux.Logger.PrintToUser(txt)
		}
		return nil
	}

	// flag not provided
	if !yes {
		ux.Logger.PrintToUser("We found a new version of Avalanche-CLI %s upstream. You are running %s", latest, thisVFmt)
		y, err := app.Prompt.CaptureYesNo("Do you want to update?")
		if err != nil {
			return err
		}
		if !y {
			ux.Logger.PrintToUser("Aborted by user")
			return ErrUserAbortedInstallation
		}
	}

	// where is the tool running from?
	ex, err := os.Executable()
	if err != nil {
		return err
	}
	execPath := filepath.Dir(ex)

	/* #nosec G204 */
	downloadCmd := exec.Command("curl", "-sSfL", constants.CliInstallationURL)

	// -s is for the sh command, -- separates the args for our install script,
	// -n skips shell completion installation, which would result in an error,
	// as it requires to launch the binary, but we are already executing it
	installCmdArgs := []string{"-s", "--", "-n"}

	// custom installation path when not executing from tempdir (go run)
	if !strings.HasPrefix(execPath, os.TempDir()) {
		installCmdArgs = append(installCmdArgs, "-b", execPath)
	}

	app.Log.Debug("installing new version", zap.String("path", execPath))

	installCmd := exec.Command("sh", installCmdArgs...)

	// redirect the download command to the install
	var downloadOutbuf, downloadErrbuf strings.Builder
	downloadCmdStdoutPipe, err := downloadCmd.StdoutPipe()
	if err != nil {
		return err
	}
	downloadCmd.Stderr = &downloadErrbuf
	installCmd.Stdin = io.TeeReader(downloadCmdStdoutPipe, &downloadOutbuf)

	// we are going to collect the output from the command into a string
	// instead of writing directly to the string
	var installOutbuf, installErrbuf strings.Builder
	installCmd.Stdout = &installOutbuf
	installCmd.Stderr = &installErrbuf

	ux.Logger.PrintToUser("Starting update...")
	if err := installCmd.Start(); err != nil {
		return err
	}
	ux.Logger.PrintToUser("Downloading install script...")
	if err := downloadCmd.Run(); err != nil {
		if downloadOutbuf.String() != "" {
			ux.Logger.PrintToUser(strings.TrimSuffix(downloadOutbuf.String(), "\n"))
		}
		if downloadErrbuf.String() != "" {
			ux.Logger.PrintToUser(strings.TrimSuffix(downloadErrbuf.String(), "\n"))
		}
		return err
	}
	ux.Logger.PrintToUser("Installing new release...")
	if err := installCmd.Wait(); err != nil {
		if installOutbuf.String() != "" {
			ux.Logger.PrintToUser(strings.TrimSuffix(installOutbuf.String(), "\n"))
		}
		if installErrbuf.String() != "" {
			ux.Logger.PrintToUser(strings.TrimSuffix(installErrbuf.String(), "\n"))
		}
		ux.Logger.PrintToUser("installation failed: %s", err.Error())
		return err
	}

	// write to file when last updated
	lastActs, err = app.ReadLastActionsFile()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			lastActs = &application.LastActions{}
		}
	}

	lastActs.LastUpdated = time.Now()
	app.WriteLastActionsFile(lastActs)

	app.Log.Debug(installOutbuf.String())
	app.Log.Debug(installErrbuf.String())

	ux.Logger.PrintToUser("Installation successful. Please run the shell completion update manually after this process terminates.")
	ux.Logger.PrintToUser("The new version will be used on next command execution")
	return nil
}
