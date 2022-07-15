package driverhyperv

const (
	driverName        = "hyperv"
	driverDescription = "Kutti driver for Hyper-V"
)

// Driver implements the drivercore.Driver interface for Hyper-V.
type Driver struct {
	powershellpath string
	scriptpath     string
	validated      bool
	status         string
	errormessage   string
}

// Name returns "hyperv"
func (vd *Driver) Name() string {
	return driverName
}

// Description returns "Kutti driver for Hyper-V"
func (vd *Driver) Description() string {
	return driverDescription
}

// UsesPerClusterNetworking returns false
func (vd *Driver) UsesPerClusterNetworking() bool {
	return false
}

// UsesNATNetworking returns false
func (vd *Driver) UsesNATNetworking() bool {
	return false
}

func (vd *Driver) validate() bool {
	if vd.validated {
		return true
	}

	// find PowerShell
	pspath, err := findPowerShell()
	if err != nil {
		vd.status = "Error"
		vd.errormessage = err.Error()
		return false
	}
	vd.powershellpath = pspath

	// Find hypervmanage script
	scriptpath, err := findScript()
	if err != nil {
		vd.status = "Error"
		vd.errormessage = err.Error()
		return false
	}
	vd.scriptpath = scriptpath

	// Check driver status
	driverstatus, err := vd.runwithresults("checkdriver")
	if err != nil {
		vd.status = "Error"
		vd.errormessage = err.Error()
		return false
	}

	if !driverstatus.Success {
		vd.status = "Error"
		vd.errormessage = driverstatus.ErrorMessage
		return false
	}

	vd.status = "Ready"
	vd.validated = true
	return true
}

// Status returns current driver status
func (vd *Driver) Status() string {
	vd.validate()
	return vd.status
}

func (vd *Driver) Error() string {
	vd.validate()
	return vd.errormessage
}
