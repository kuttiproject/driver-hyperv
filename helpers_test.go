package driverhyperv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/workspace"
)

// quietlogger is a kuttilog logger that discards everything, to keep the
// output of the driver off the output of the tests.
type quietlogger struct{}

func (quietlogger) MaxLevel() int                     { return kuttilog.Debug }
func (quietlogger) LevelPrefix(level int) string      { return "" }
func (quietlogger) Print(v ...interface{})            {}
func (quietlogger) Printf(f string, v ...interface{}) {}
func (quietlogger) Println(v ...interface{})          {}

// The tests run the driver against a fake PowerShell, which is the test
// binary itself. TestMain turns the binary into the fake when
// fakePowerShellEnv is set, which the tests do, through testStorage, only
// for the processes they start. That works the same on every platform, and
// needs no Hyper-V.
const fakePowerShellEnv = "KUTTI_TEST_FAKE_POWERSHELL"

func TestMain(m *testing.M) {
	if os.Getenv(fakePowerShellEnv) != "" {
		os.Exit(fakePowerShell(os.Args[1:]))
	}

	os.Exit(m.Run())
}

// fakeresult has the fields of what hypervmanage.ps1 prints. The script
// spells the last one "PayLoad".
type fakeresult struct {
	Success      bool
	ErrorMessage string
	PayLoad      map[string]interface{}
}

func printFake(result fakeresult) int {
	data, err := json.MarshalIndent(result, "", "    ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	fmt.Println(string(data))
	return 0
}

// fakePowerShell stands in for PowerShell running hypervmanage.ps1. It
// appends its arguments, joined with spaces, as a line to the file named by
// FAKE_PS_LOG. It insists on being called as the driver calls PowerShell:
//
//	-NoProfile -NonInteractive -File <script> <command> [<argument>...]
//
// "checkdriver" answers according to FAKE_PS_CHECKDRIVER:
//
//	ok (or not set)  Success
//	unavailable      Success false, with the message the script gives when
//	                 Hyper-V is not enabled
//	garbage          something that is not JSON
//	exit             exit with an error
//
// "getvhdparent <disk>" answers according to files next to the disk:
//
//	<disk>.fail         exit with an error
//	<disk>.garbage      print something that is not JSON
//	<disk>.unsuccessful Success false, with an error message
//	<disk>.payloadless  Success, without a payload
//	<disk>.parent       contains the parent path to report
//
// With none of these, it reports a disk without a parent.
func fakePowerShell(args []string) int {
	if logpath := os.Getenv("FAKE_PS_LOG"); logpath != "" {
		f, err := os.OpenFile(logpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
		if err == nil {
			fmt.Fprintln(f, strings.Join(args, " "))
			f.Close()
		}
	}

	if len(args) < 5 || args[0] != "-NoProfile" || args[1] != "-NonInteractive" || args[2] != "-File" {
		fmt.Fprintln(os.Stderr, "fake powershell: unexpected arguments:", args)
		return 2
	}

	rest := args[5:]

	switch strings.ToLower(args[4]) {
	case "checkdriver":
		switch os.Getenv("FAKE_PS_CHECKDRIVER") {
		case "unavailable":
			return printFake(fakeresult{ErrorMessage: "Hyper-V not enabled"})
		case "garbage":
			fmt.Println("this is not json")
			return 0
		case "exit":
			fmt.Fprintln(os.Stderr, "fake powershell: failed")
			return 1
		}

		return printFake(fakeresult{
			Success: true,
			PayLoad: map[string]interface{}{"HypervisorPresent": true, "Permissions": true, "PermissionLevel": "administrator"},
		})

	case "getvhdparent":
		if len(rest) == 0 {
			return printFake(fakeresult{ErrorMessage: "vhdpath not specified"})
		}
		disk := rest[0]

		exists := func(suffix string) bool {
			_, err := os.Stat(disk + suffix)
			return err == nil
		}

		switch {
		case exists(".fail"):
			fmt.Fprintf(os.Stderr, "fake powershell: could not open '%s'\n", disk)
			return 1
		case exists(".garbage"):
			fmt.Println("this is not json")
			return 0
		case exists(".unsuccessful"):
			return printFake(fakeresult{ErrorMessage: "The file is in use"})
		case exists(".payloadless"):
			return printFake(fakeresult{Success: true})
		}

		parent := ""
		if data, err := os.ReadFile(disk + ".parent"); err == nil {
			parent = strings.TrimSpace(string(data))
		}

		vhdtype := "Dynamic"
		if parent != "" {
			vhdtype = "Differencing"
		}

		return printFake(fakeresult{
			Success: true,
			PayLoad: map[string]interface{}{"ParentPath": parent, "VhdType": vhdtype},
		})
	}

	return printFake(fakeresult{ErrorMessage: "invalid interface argument: " + args[4]})
}

// testStorage points the kutti workspace at a new temporary directory for the
// duration of a test, and makes the driver use the fake PowerShell. It
// returns the workspace directory. The workspace and the driver are global,
// so tests that use this must not run in parallel.
func testStorage(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := workspace.Set(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(workspace.Reset)

	kuttilog.SetLogger(quietlogger{})
	t.Cleanup(kuttilog.ResetLogger)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	saved := findPowerShell
	findPowerShell = func() (string, error) { return exe, nil }
	t.Cleanup(func() { findPowerShell = saved })

	t.Setenv(fakePowerShellEnv, "1")
	t.Setenv("FAKE_PS_LOG", filepath.Join(t.TempDir(), "calls.log"))
	t.Setenv("FAKE_PS_CHECKDRIVER", "ok")

	return root
}

// noPowerShell makes the driver find no PowerShell.
func noPowerShell(t *testing.T) {
	t.Helper()

	saved := findPowerShell
	findPowerShell = func() (string, error) { return "", fmt.Errorf("PowerShell not found") }
	t.Cleanup(func() { findPowerShell = saved })
}

// psCalls returns the calls made to the fake PowerShell so far, one string
// for each, of the arguments joined with spaces.
func psCalls(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(os.Getenv("FAKE_PS_LOG"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}

	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// The locations of things, written out in full on purpose, so that a change
// to the layout is noticed.

func cacheDirPath(root string) string {
	return filepath.Join(root, "kutti-cache", "driver-hyperv")
}

func disksDirPath(root string) string {
	return filepath.Join(root, "kutti-cache", "driver-hyperv-disks")
}

func machinesDirPath(root string) string {
	return filepath.Join(root, "kutti-cache", "driver-hyperv-machines")
}

func listFilePath(root string) string {
	return filepath.Join(root, "kutti-config", "driver-hyperv-images.json")
}

// cachedImagePath is where the cached image for a version of Kubernetes must
// be.
func cachedImagePath(root string, version string) string {
	return filepath.Join(cacheDirPath(root), "kutti-"+version+".vhdx")
}

// addDisk creates a VM disk file in the disks directory. The fake PowerShell
// reports the given parent for it, if there is one.
func addDisk(t *testing.T, root string, name string, parent string) string {
	t.Helper()

	dir := disksDirPath(root)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}

	disk := filepath.Join(dir, name)
	if err := os.WriteFile(disk, []byte("disk"), 0o666); err != nil {
		t.Fatal(err)
	}
	if parent != "" {
		if err := os.WriteFile(disk+".parent", []byte(parent+"\n"), 0o666); err != nil {
			t.Fatal(err)
		}
	}

	return disk
}

func touch(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, nil, 0o666); err != nil {
		t.Fatal(err)
	}
}
