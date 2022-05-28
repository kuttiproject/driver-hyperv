package driverhyperv

const (
	driverName        = "hyperv"
	driverDescription = "Kutti driver for Hyper-V"
)

type Driver struct {
	powershellpath string
	scriptpath     string
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

// Status returns current driver status
func (vd *Driver) Status() string {
	return vd.status
}

func (vd *Driver) Error() string {
	return vd.errormessage
}
