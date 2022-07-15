package driverhyperv

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kuttiproject/drivercore"
)

type hypervmachinedata struct {
	Name      string
	IPAddress string
	State     string
}

// The MachineStatus* constants add some Hyper-V specific statuses.
const (
	MachineStatusStarting = drivercore.MachineStatus("Starting")
	MachineStatusStopping = drivercore.MachineStatus("Stopping")
	MachineStatusCreating = drivercore.MachineStatus("Creating")
)

// Machine implements the drivercore.Machine interface for VirtualBox
type Machine struct {
	driver *Driver

	name           string
	clustername    string
	savedipaddress string
	status         drivercore.MachineStatus
	errormessage   string
}

func (hmd *hypervmachinedata) Machine(driver *Driver) *Machine {
	var clustername string
	var machinename string
	var machinestatus drivercore.MachineStatus

	switch hmd.State {
	case "Off":
		machinestatus = drivercore.MachineStatusStopped
	case "Running":
		machinestatus = drivercore.MachineStatusRunning
	default:
		machinestatus = drivercore.MachineStatus("Unknown")
	}

	nameparts := strings.Split(hmd.Name, "-")

	if len(nameparts) > 1 {
		clustername = nameparts[0]
		machinename = nameparts[1]
	} else {
		machinename = nameparts[0]
	}

	return &Machine{
		driver:         driver,
		name:           machinename,
		clustername:    clustername,
		savedipaddress: hmd.IPAddress,
		status:         machinestatus,
	}
}

// Name is the name of the machine.
func (vh *Machine) Name() string {
	return vh.name
}

func (vh *Machine) qname() string {
	return vh.driver.QualifiedMachineName(vh.name, vh.clustername)
}

// Status can be drivercore.MachineStatusRunning, drivercore.MachineStatusStopped
// drivercore.MachineStatusUnknown, drivercore.MachineStatusError,
// driverhyperv.MachineStatusStarting or driverhyperv.MachineStatusStopping.
func (vh *Machine) Status() drivercore.MachineStatus {
	return vh.status
}

// Error returns the last error caused when manipulating this machine.
// A valid value can be expected only when Status() returns
// drivercore.MachineStatusError.
func (vh *Machine) Error() string {
	return vh.errormessage
}

// IPAddress returns the current IP Address of this Machine.
// The Machine status has to be Running. If not, returns an
// empty string.
func (vh *Machine) IPAddress() string {
	// This guestproperty is only available if the VM is
	// running, and has the Virtual Machine additions enabled
	return vh.savedipAddress()
}

// SSHAddress returns the address to SSH into this Machine.
// The Machine status has to be Running. If not, returns an
// empty string.
// In the Hyper-V driver, this is the same as the IP address,
// followed by ":22" if not blank.
func (vh *Machine) SSHAddress() string {
	ipaddress := vh.IPAddress()
	if ipaddress != "" {
		return vh.savedipAddress() + ":22"
	}
	return ""
}

// Start starts a Machine.
// It does this by running the command:
//   Start-VM -Name <machinename>
// through an interface script.
// Note that a Machine may not be ready for further operations at the end of this,
// and therefore its status will Starting, not Started.
// See WaitForStateChange().
func (vh *Machine) Start() error {
	output, err := vh.driver.runwithresults(
		"startmachine",
		vh.qname(),
	)

	if err != nil {
		return fmt.Errorf("could not start the host '%s': %v", vh.name, err)
	}

	if !output.Success {
		return fmt.Errorf("could not start the host '%s': %v", vh.name, output.ErrorMessage)
	}

	vh.status = MachineStatusStarting

	return nil
}

// Stop stops a Machine.
// It does this by running the command:
//   Stop-VM -Name <machinename> -Force
// Note that a Machine may not be ready for further operations at the end of this,
// and therefore its status will be Stopping, not Stopped.
// See WaitForStateChange().
func (vh *Machine) Stop() error {
	output, err := vh.driver.runwithresults(
		"stopmachine",
		vh.qname(),
	)

	if err != nil {
		return fmt.Errorf("could not stop the host '%s': %v", vh.name, err)
	}

	if !output.Success {
		return fmt.Errorf("could not stop the host '%s': %v", vh.name, output.ErrorMessage)
	}

	vh.status = MachineStatusStopping

	return nil
}

// ForceStop stops a Machine forcibly.
// It does this by running the command:
//   Stop-VM -Name <machinename> -TurnOff
// through an interface script.
// This operation will set the status to drivercore.MachineStatusStopped.
func (vh *Machine) ForceStop() error {
	output, err := vh.driver.runwithresults(
		"forcestopmachine",
		vh.qname(),
	)

	if err != nil {
		return fmt.Errorf("could not force stop the host '%s': %v", vh.name, err)
	}

	if !output.Success {
		return fmt.Errorf("could not force stop the host '%s': %v", vh.name, output.ErrorMessage)
	}

	vh.status = drivercore.MachineStatusStopped
	return nil
}

// WaitForStateChange waits the specified number of seconds,
// or until the Machine status changes.
// It does this by running the command:
//   Wait-VM -ErrorAction Stop -VMName $machineName -Timeout $timeOutSeconds -For IPAddress
// if the Machine is starting, or the command:
//   Wait-VM -ErrorAction Stop -VMName $machineName -Timeout $timeOutSeconds -For Reboot
// if the Macine is stopping.
// WaitForStateChange should be called after a call to Start, before
// any other operation. From observation, it should not be called _before_ Stop.
func (vh *Machine) WaitForStateChange(timeoutinseconds int) {
	result, _ := vh.driver.runwithresults("waitmachine", vh.qname(), string(vh.status), "25")
	if result.Success {
		vh.fromdriverresult(result)
	}
}

// ForwardPort is not supported for the Hyper-V driver.
func (vh *Machine) ForwardPort(hostport int, machineport int) error {
	return nil
}

// UnforwardPort is not supported for the Hyper-V driver.
func (vh *Machine) UnforwardPort(machineport int) error {
	return nil
}

// ForwardSSHPort is not supported for the Hyper-V driver.
func (vh *Machine) ForwardSSHPort(hostport int) error {
	return nil
}

// ImplementsCommand returns true if the driver implements the specified predefined command.
// The Hyper-V driver implements drivercore.RenameMachine
func (vh *Machine) ImplementsCommand(command drivercore.PredefinedCommand) bool {
	_, ok := hypervCommands[command]
	return ok

}

// ExecuteCommand executes the specified predefined command.
func (vh *Machine) ExecuteCommand(command drivercore.PredefinedCommand, params ...string) error {
	commandfunc, ok := hypervCommands[command]
	if !ok {
		return fmt.Errorf(
			"command '%v' not implemented",
			command,
		)
	}

	return commandfunc(vh, params...)
}

func (vh *Machine) get() error {
	output, err := vh.driver.runwithresults("getmachine", vh.qname())
	if err != nil {
		return err
	}

	return vh.fromdriverresult(output)
}

func (vh *Machine) fromdriverresult(output *driverresult) error {
	machinedatamap, ok := output.Payload["Machine"].(map[string]interface{})
	if !ok {
		return errors.New("could not get machine data: interface error")
	}
	machinename := machinedatamap["Name"].(string)
	machineip := machinedatamap["IPAddress"].(string)
	machinestate := machinedatamap["State"].(string)

	machinedata := &hypervmachinedata{
		Name:      machinename,
		IPAddress: machineip,
		State:     machinestate,
	}

	tempResult := machinedata.Machine(vh.driver)

	vh.name = tempResult.name
	vh.clustername = tempResult.clustername
	vh.savedipaddress = tempResult.savedipaddress
	vh.status = tempResult.status

	return nil
}

func (vh *Machine) savedipAddress() string {
	// This guestproperty is set when the VM is created
	if vh.savedipaddress != "" {
		return vh.savedipaddress
	}
	vh.get()

	return vh.savedipaddress
}
