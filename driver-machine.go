package driverhyperv

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kuttiproject/drivercore"
	"github.com/kuttiproject/kuttilog"
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
	var firstErr error

	// Delete machine disk
	destdir, err := diskDir()
	if err == nil {
		destfile := filepath.Join(destdir, qualifiedmachinename+".vhdx")
		if err := os.Remove(destfile); err != nil && !os.IsNotExist(err) {
			firstErr = err
		}
	}

	// Delete VM directory
	machinepathbase, err := machineDir()
	if err == nil {
		machinepath := filepath.Join(machinepathbase, qualifiedmachinename)
		if err := os.RemoveAll(machinepath); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
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

	kuttilog.Printf(kuttilog.Info, "Deleting machine '%s'...", machinename)
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
		kuttilog.Printf(kuttilog.Debug, "Notice: could not delete some machine files for %s: %v", machinename, err)
	}

	return nil
}

// NewMachine creates a VM.
// It also starts the VM, changes the hostname, saves the IP address, and stops
// it again.
// It uses a differencing VHDX file backed by the cached master image for the
// specified k8sversion in the driver cache location for VM disks.
// It then runs the following Cmdlets, in order:
//   New-VHD -Path $diffVhdPath -ParentPath $parentVhdPath -Differencing
//   $newvm = New-VM -Name $machineName -Generation 1 -Path $machinePath -VHDPath $diffVhdPath -SwitchName "Default Switch"
//   Set-VM $newvm -StaticMemory -MemoryStartupBytes 2147483648 -ProcessorCount 2 -CheckpointType Disabled
// through an interface script.
func (vd *Driver) NewMachine(machinename string, clustername string, k8sversion string) (drivercore.Machine, error) {
	if !vd.validate() {
		return nil, vd
	}

	qualifiedmachinename := vd.QualifiedMachineName(machinename, clustername)

	// Fail fast if a machine with this name already exists
	if existingMachine, err := vd.GetMachine(machinename, clustername); err == nil && existingMachine != nil {
		return nil, fmt.Errorf("machine %s already exists in cluster %s", machinename, clustername)
	}

	// 1. Get the local cached base image
	kuttilog.Println(kuttilog.Info, "Verifying base image...")
	vhdfile, err := imagepathfromk8sversion(k8sversion)
	if err != nil {
		return nil, err
	}
	vhdfile, err = filepath.Abs(vhdfile)
	if err != nil {
		return nil, err
	}

	if _, err = os.Stat(vhdfile); err != nil {
		return nil, fmt.Errorf("cached image not found for K8s version %s at %s: %v", k8sversion, vhdfile, err)
	}

	// 2. Prepare VM differencing disk path
	destdir, err := diskDir()
	if err != nil {
		return nil, err
	}

	destfile := filepath.Join(destdir, qualifiedmachinename+".vhdx")
	destfile, err = filepath.Abs(destfile)
	if err != nil {
		return nil, err
	}

	// Clear leftover disk file from any previous failed run
	_ = os.Remove(destfile)

	machinepath, err := machineDir()
	if err != nil {
		return nil, err
	}
	machinepath, err = filepath.Abs(machinepath)
	if err != nil {
		return nil, err
	}

	// 3. Create differencing disk and VM
	kuttilog.Println(kuttilog.Info, "Creating differencing disk and VM...")
	newmachine := &Machine{
		driver:      vd,
		name:        machinename,
		clustername: clustername,
		status:      drivercore.MachineStatus("Creating"),
	}

	result, err := vd.runwithresults("newmachine", qualifiedmachinename, machinepath, destfile, vhdfile)
	if err != nil {
		_ = deletemachinefiles(qualifiedmachinename)
		return nil, fmt.Errorf("could not create host '%v': %v", machinename, err)
	}

	if !result.Success {
		_ = deletemachinefiles(qualifiedmachinename)
		return nil, fmt.Errorf("could not create host '%v': %v", machinename, result.ErrorMessage)
	}

	// 4. Start the host
	kuttilog.Println(kuttilog.Info, "Starting host...")
	err = newmachine.Start()
	if err != nil {
		kuttilog.Printf(kuttilog.Info, "Failed to start host: %v. Rolling back...", err)
		if delErr := vd.DeleteMachine(machinename, clustername); delErr != nil {
			kuttilog.Printf(kuttilog.Info, "Rollback also failed: %v", delErr)
		}
		return nil, fmt.Errorf("could not start machine %s: %v", machinename, err)
	}

	// TODO: Try to parameterize the timeout
	newmachine.WaitForStateChange(25)

	// 5. Fetch IP Address
	ipSet := false
	for ipretries := 1; ipretries < 4; ipretries++ {
		kuttilog.Printf(kuttilog.Info, "Fetching IP address (attempt %v/3)...", ipretries)

		if newmachine.savedipaddress != "" {
			kuttilog.Printf(kuttilog.Info, "Obtained IP address '%v'", newmachine.savedipaddress)
			ipSet = true
			break
		}

		kuttilog.Printf(kuttilog.Info, "Failed. Waiting %v seconds before retry...", ipretries*10)
		time.Sleep(time.Duration(ipretries*10) * time.Second)

		_ = newmachine.get()
	}

	if !ipSet {
		kuttilog.Println(kuttilog.Info, "Warning: Failed to get IP address. You may have to delete this node and recreate it manually.")
	}

	// 6. Change the hostname
	for renameretries := 1; renameretries < 4; renameretries++ {
		kuttilog.Printf(kuttilog.Info, "Renaming host (attempt %v/3)...", renameretries)
		err = newmachine.ExecuteCommand(drivercore.RenameMachine, machinename)
		if err == nil {
			break
		}
		kuttilog.Printf(kuttilog.Info, "Failed. Waiting %v seconds before retry...", renameretries*10)
		time.Sleep(time.Duration(renameretries*10) * time.Second)
	}

	if err != nil {
		kuttilog.Printf(kuttilog.Info, "Failed to rename host after 3 attempts: %v. Rolling back...", err)
		if delErr := vd.DeleteMachine(machinename, clustername); delErr != nil {
			kuttilog.Printf(kuttilog.Info, "Rollback also failed: %v", delErr)
		}
		return nil, fmt.Errorf("could not rename machine %s: %v", machinename, err)
	}
	kuttilog.Println(kuttilog.Info, "Host renamed.")

	// 7. Stop host
	kuttilog.Println(kuttilog.Info, "Stopping host...")
	_ = newmachine.Stop()

	newmachine.status = drivercore.MachineStatusStopped

	return newmachine, nil
}
