package driverhyperv

import (
	"github.com/kuttiproject/drivercore"
)

// QualifiedNetworkName is not implemented in the Hyper-V driver.
func (vd *Driver) QualifiedNetworkName(clustername string) string {
	panic("not implemented")
}

// DeleteNetwork is not implemented in the Hyper-V driver.
func (vd *Driver) DeleteNetwork(clustername string) error {
	panic("not implemented")
}

// NewNetwork is not implemented in the Hyper-V driver.
func (vd *Driver) NewNetwork(clustername string) (drivercore.Network, error) {
	panic("not implemented")
}
