package driverhyperv

import (
	"github.com/kuttiproject/drivercore"
)

func init() {
	driver := newhypervdriver()

	drivercore.RegisterDriver(driverName, driver)
}

func newhypervdriver() *Driver {
	result := &Driver{}

	// find PowerShell
	pspath, err := findPowerShell()
	if err != nil {
		result.status = "Error"
		result.errormessage = err.Error()
		return result
	}

	result.powershellpath = pspath

	// Find hypervmanage script
	scriptpath, err := findScript()
	if err != nil {
		result.status = "Error"
		result.errormessage = err.Error()
		return result
	}
	result.scriptpath = scriptpath

	// Check driver status
	driverstatus, err := result.runwithresults("checkdriver")
	if err != nil {
		result.status = "Error"
		result.errormessage = err.Error()
		return result
	}

	if !driverstatus.Success {
		result.status = "Error"
		result.errormessage = driverstatus.ErrorMessage
		return result
	}

	result.status = "Ready"
	return result
}
