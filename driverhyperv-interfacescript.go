package driverhyperv

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kuttiproject/workspace"
)

type driverresult struct {
	Success      bool
	ErrorMessage string
	Payload      map[string]interface{}
}

const scriptVersion = "0.1"

var scriptname = "hypervmanage-" + scriptVersion + ".ps1"

func findPowerShell() (string, error) {
	// First, try looking up Windows PowerShell on the path
	toolpath, err := exec.LookPath("powershell.exe")
	if err == nil {
		return toolpath, nil
	}

	// If not, look for cross-platform PowerShell
	toolpath, err = exec.LookPath("pwsh.exe")
	if err == nil {
		return toolpath, nil
	}

	return "", errors.New("PowerShell not found")
}

func machineDir() (string, error) {
	return workspace.Cachesubdir("driver-hyperv-machines")
}

func diskDir() (string, error) {
	return workspace.Cachesubdir("driver-hyperv-disks")
}

func findScript() (string, error) {
	scriptdir, err := hypervCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not find script: %v", err.Error())
	}

	scriptpath := filepath.Join(scriptdir, scriptname)
	if _, err := os.Stat(scriptpath); err != nil {
		err = writeScript(scriptpath)
		if err != nil {
			return "", err
		}
	}

	return scriptpath, nil
}

//go:embed assets/hypervmanage.ps1
var script string

func writeScript(scriptpath string) error {
	scriptFile, err := os.Create(scriptpath)
	if err != nil {
		return err
	}

	defer scriptFile.Close()

	_, err = scriptFile.WriteString(script)
	if err != nil {
		return err
	}

	return nil
}

func (vd *Driver) runwithresults(args ...string) (*driverresult, error) {
	// if !vd.validate() {
	// 	return nil, vd
	// }

	powershellargs := []string{
		"-NoProfile",
		"-NonInteractive",
		"-File",
		vd.scriptpath,
	}
	powershellargs = append(powershellargs, args...)
	resultstring, err := workspace.Runwithresults(vd.powershellpath, powershellargs...)
	if err != nil {
		return nil, err
	}

	dr := &driverresult{}
	err = json.Unmarshal([]byte(resultstring), dr)
	if err != nil {
		return nil, err
	}

	return dr, nil
}
