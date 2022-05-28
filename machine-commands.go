package driverhyperv

import (
	"fmt"
	"strings"

	"github.com/kuttiproject/drivercore"
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
	client := sshclient.NewWithPassword(hypervUsername, hypervPassword)
	params := append([]string{execpath}, paramarray...)
	output, err := client.RunWithResults(vh.SSHAddress(), strings.Join(params, " "))
	if err != nil {
		return "", err
	}
	return output, nil
}

var hypervCommands = map[drivercore.PredefinedCommand]func(*Machine, ...string) error{
	drivercore.RenameMachine: renamemachine,
}

func renamemachine(vh *Machine, params ...string) error {
	newname := params[0]
	execname := fmt.Sprintf("/home/%s/kutti-installscripts/set-hostname.sh", hypervUsername)

	_, err := vh.runwithresults(
		"/usr/bin/sudo",
		execname,
		newname,
	)

	return err
}
