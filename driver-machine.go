package driverhyperv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kuttiproject/drivercore"
	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/workspace"
)

func currentusershortname() string {
	// Windows populates the environment variable USERNAME with the login name of the
	// current user.
	return os.ExpandEnv("$USERNAME")
}

// QualifiedMachineName returns a name in the form <username>-<clustername>-<machinename>.
// The <username> part is needed for this driver because Hyper-V VM names are machine-wide.
// It separates nodes created by different users.
func (vd *Driver) QualifiedMachineName(machinename string, clustername string) string {
	return fmt.Sprintf("%v-%v-%v", currentusershortname(), clustername, machinename)
}

// ListMachines returns a list of VMS.
// It does this by running the Cmdlet:
//   Get-VM
// through an interface script.
func (vd *Driver) ListMachines() ([]drivercore.Machine, error) {
	if !vd.validate() {
		return nil, vd
	}

	output, err := vd.runwithresults("listmachines")
	if err != nil {
		return nil, fmt.Errorf("could not get list of VMs: %v", err)
	}

	resultarr, ok := output.Payload["VMList"].([]hypervmachinedata)
	if !ok {
		return nil, errors.New("could not get list of VMs: interface error")
	}

	finalresult := make([]drivercore.Machine, 0, len(resultarr))
	for _, item := range resultarr {
		finalresult = append(finalresult, item.Machine(vd))
	}
	return finalresult, nil
}

// GetMachine returns the named machine, or an error.
// It does this by running the Cmdlet:
//   Get-VM -Name <machinename>
// through an interface script.
func (vd *Driver) GetMachine(machinename string, clustername string) (drivercore.Machine, error) {
	if !vd.validate() {
		return nil, vd
	}

	machine := &Machine{
		driver:      vd,
		name:        machinename,
		clustername: clustername,
		status:      drivercore.MachineStatusUnknown,
	}

	err := machine.get()

	if err != nil {
		return nil, err
	}

	return machine, nil
}

func deletemachinefiles(qualifiedmachinename string) error {
	// Delete machine disk
	destdir, _ := diskDir()
	destfile := filepath.Join(destdir, qualifiedmachinename+".vhdx")
	err := os.Remove(destfile)
	if err != nil {
		return err
	}

	// Delete VM directory
	machinepathbase, _ := machineDir()
	machinepath := filepath.Join(machinepathbase, qualifiedmachinename)
	err = os.RemoveAll(machinepath)
	if err != nil {
		return err
	}

	return nil
}

// DeleteMachine completely deletes a Machine.
// It does this by running the Cmdlet:
//   Remove-VM -Name <machinename> -Force
// through an interface script.
// It also deletes the VM disk files and the directory containing the VM files.
func (vd *Driver) DeleteMachine(machinename string, clustername string) error {
	if !vd.validate() {
		return vd
	}

	qualifiedmachinename := vd.QualifiedMachineName(machinename, clustername)
	output, err := vd.runwithresults(
		"deletemachine",
		qualifiedmachinename,
	)

	if err != nil {
		return fmt.Errorf("could not delete machine %s: %v", machinename, err)
	}

	if !output.Success {
		return fmt.Errorf("could not delete machine %s: %v", machinename, output.ErrorMessage)
	}

	err = deletemachinefiles(qualifiedmachinename)
	if err != nil {
		return err
	}

	return nil
}

//var ipRegex, _ = regexp.Compile(`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$`)

// NewMachine creates a VM.
// It also starts the VM, changes the hostname, saves the IP address, and stops
// it again.
// It starts by copying the VHDX file appropriate for the specified k8sversion
// to the driver cache location for VM disks.
// It then runs the following Cmdlets, in order:
//   $newvm New-VM -Name $machineName -Generation 1 -Path $machinePath -VHDPath $vhdpath -SwitchName "Default Switch"
//   Set-VM $newvm -StaticMemory -MemoryStartupBytes 2147483648 -ProcessorCount 2 -CheckpointType Disabled
// through an interface script.
// The first creates a Hyper-V "Generation 1" VM which uses the VHDX file metntioned
// above, and connects it to the Hyper-V default network switch.
// The second turns off dynamic memory and checkpoints on the VM, and sets memory
// to 2GB and core count to 2 (hardcoded for now).
func (vd *Driver) NewMachine(machinename string, clustername string, k8sversion string) (drivercore.Machine, error) {
	if !vd.validate() {
		return nil, vd
	}

	qualifiedmachinename := vd.QualifiedMachineName(machinename, clustername)

	kuttilog.Println(kuttilog.Info, "Importing image...")

	vhdfile, err := imagepathfromk8sversion(k8sversion)
	if err != nil {
		return nil, err
	}

	if _, err = os.Stat(vhdfile); err != nil {
		return nil, fmt.Errorf("could not retrieve image %s: %v", vhdfile, err)
	}

	destdir, err := diskDir()
	if err != nil {
		return nil, err
	}

	destfile := filepath.Join(destdir, qualifiedmachinename+".vhdx")
	err = workspace.CopyFile(vhdfile, destfile, 524288000, true)
	if err != nil {
		return nil, fmt.Errorf("could not import image %s: %v", vhdfile, err)
	}

	// Create new VM
	machinepath, _ := machineDir()

	newmachine := &Machine{
		driver:      vd,
		name:        machinename,
		clustername: clustername,
		status:      drivercore.MachineStatus("Creating"),
	}

	result, err := vd.runwithresults("newmachine", qualifiedmachinename, machinepath, destfile)
	if err != nil {
		return nil, fmt.Errorf("could not create host '%v': %v", machinename, err)
	}

	if !result.Success {
		deletemachinefiles(qualifiedmachinename)

		return nil, fmt.Errorf("could not create host '%v': %v", machinename, result.ErrorMessage)
	}

	// Start the host
	kuttilog.Println(kuttilog.Info, "Starting host...")
	err = newmachine.Start()
	if err != nil {
		return newmachine, err
	}
	// TODO: Try to parameterize the timeout
	newmachine.WaitForStateChange(25)

	// Save the IP Address
	// The first IP address should be DHCP-assigned.
	// This may fail if we check too soon. So, we check
	// up to three times.
	ipSet := false
	for ipretries := 1; ipretries < 4; ipretries++ {
		kuttilog.Printf(kuttilog.Info, "Fetching IP address (attempt %v/3)...", ipretries)

		if newmachine.savedipaddress != "" {
			// TODO: verify IP address here
			kuttilog.Printf(kuttilog.Info, "Obtained IP address '%v'", newmachine.savedipaddress)
			ipSet = true
			break
		}

		kuttilog.Printf(kuttilog.Info, "Failed. Waiting %v seconds before retry...", ipretries*10)
		time.Sleep(time.Duration(ipretries*10) * time.Second)

		newmachine.get()
	}

	if !ipSet {
		kuttilog.Printf(0, "Error: Failed to get IP address. You may have to delete this node and recreate it manually.")
	}

	// Change the name
	for renameretries := 1; renameretries < 4; renameretries++ {
		kuttilog.Printf(kuttilog.Info, "Renaming host (attempt %v/3)...", renameretries)
		err = renamemachine(newmachine, machinename)
		if err == nil {
			break
		}
		kuttilog.Printf(kuttilog.Info, "Failed. Waiting %v seconds before retry...", renameretries*10)
		time.Sleep(time.Duration(renameretries*10) * time.Second)
	}

	if err != nil {
		return newmachine, err
	}
	kuttilog.Println(kuttilog.Info, "Host renamed.")

	kuttilog.Println(kuttilog.Info, "Stopping host...")
	newmachine.Stop()
	// newhost.WaitForStateChange(25)

	newmachine.status = drivercore.MachineStatusStopped

	return newmachine, nil
}
