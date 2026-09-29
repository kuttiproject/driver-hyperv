package driverhyperv

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kuttiproject/drivercore"
	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/sshclient"
)

// TODO: Look at parameterizing these
var (
	hypervUsername = "kuttiadmin"
	hypervPassword = "Pass@word1"
)

// runwithresults allows running commands inside a VM Host.
// It does this by creating an SSH session with the host.
func (vh *Machine) runwithresults(execpath string, paramarray ...string) (string, error) {
	sshAddr := vh.SSHAddress()
	if sshAddr == "" {
		return "", fmt.Errorf("machine %s does not have an SSH address", vh.name)
	}

	client := sshclient.NewWithPassword(hypervUsername, hypervPassword)
	params := append([]string{execpath}, paramarray...)
	cmdLine := strings.Join(params, " ")
	kuttilog.Printf(kuttilog.Debug, "Executing command over SSH to %s: %s", sshAddr, cmdLine)
	output, err := client.RunWithResults(sshAddr, cmdLine)
	if err != nil {
		kuttilog.Printf(kuttilog.Debug, "SSH command error: %v, output: %s", err, output)
		return output, err
	}
	kuttilog.Printf(kuttilog.Debug, "SSH command output: %s", output)
	return output, nil
}

var hypervCommands = map[drivercore.PredefinedCommand]func(*Machine, ...string) error{
	drivercore.RenameMachine: renamemachine,
}

func renamemachine(vh *Machine, params ...string) error {
	if len(params) == 0 {
		return errors.New("missing new hostname parameter")
	}
	newname := params[0]
	execname := "/opt/kutti/scripts/set-hostname.sh"

	_, err := vh.runwithresults(
		"/usr/bin/sudo",
		execname,
		newname,
	)

	return err
}
