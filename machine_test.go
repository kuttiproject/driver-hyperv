package driverhyperv

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kuttiproject/drivercore"
)

// A machine is made from the cached image for its version. Without one, it is
// not made, and no disk or machine is created.
func TestNewMachineNeedsTheCachedImage(t *testing.T) {
	root := testStorage(t)

	machine, err := (&Driver{}).NewMachine("node1", "cluster1", "1.37")
	if err == nil || machine != nil {
		t.Fatalf("NewMachine returned %v, %v", machine, err)
	}

	expected := "cached image not found for K8s version 1.37 at " + cachedImagePath(root, "1.37")
	if !strings.Contains(err.Error(), expected) {
		t.Errorf("the error is %q, expected it to contain %q", err, expected)
	}

	for _, call := range psCalls(t) {
		if !strings.HasSuffix(call, " checkdriver") && !strings.Contains(call, " getmachine ") {
			t.Errorf("unexpected PowerShell call: %s", call)
		}
	}
}

// What is not a version is not made into a path.
func TestNewMachineRefusesAVersionThatIsNotAVersion(t *testing.T) {
	testStorage(t)

	for _, version := range []string{"../1.37", "1.37/../../x", ""} {
		machine, err := (&Driver{}).NewMachine("node1", "cluster1", version)
		if err == nil || machine != nil {
			t.Errorf("version %q: NewMachine returned %v, %v", version, machine, err)
		}
		if err != nil && strings.Contains(err.Error(), "cached image not found") {
			t.Errorf("version %q was made into a path: %v", version, err)
		}
	}

	// It stopped before it made anything.
	for _, call := range psCalls(t) {
		if !strings.HasSuffix(call, " checkdriver") && !strings.Contains(call, " getmachine ") {
			t.Errorf("unexpected PowerShell call: %s", call)
		}
	}
}

func newWaitTestMachine(t *testing.T, status drivercore.MachineStatus) *Machine {
	t.Helper()

	d := &Driver{}
	if !d.validate() {
		t.Fatalf("the driver is not valid: %s", d.errormessage)
	}

	return &Machine{driver: d, name: "node1", clustername: "cluster1", status: status}
}

// The script is told the machine, its status, and how long to wait.
func TestWaitForStateChangeTellsTheScriptHowLongToWait(t *testing.T) {
	root := testStorage(t)
	script := filepath.Join(cacheDirPath(root), scriptname)

	for _, status := range []drivercore.MachineStatus{MachineStatusStarting, MachineStatusStopping, drivercore.MachineStatusRunning} {
		for _, timeout := range []int{5, 20, 120} {
			machine := newWaitTestMachine(t, status)
			machine.WaitForStateChange(timeout)

			expected := "-NoProfile -NonInteractive -File " + script + " waitmachine " + machine.qname() + " " + string(status) + " " + strconv.Itoa(timeout)
			calls := psCalls(t)
			if got := calls[len(calls)-1]; got != expected {
				t.Errorf("status %s, timeout %d: PowerShell was called as\n%s\nexpected\n%s", status, timeout, got, expected)
			}
		}
	}
}

// If the wait works, the machine is brought up to date from what it returns.
func TestWaitForStateChangeUpdatesTheMachine(t *testing.T) {
	cases := []struct {
		name       string
		status     drivercore.MachineStatus
		state      string
		wantstatus drivercore.MachineStatus
	}{
		{"started", MachineStatusStarting, "Running", drivercore.MachineStatusRunning},
		{"stopped", MachineStatusStopping, "Off", drivercore.MachineStatusStopped},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testStorage(t)
			t.Setenv("FAKE_PS_WAIT_STATE", tc.state)

			machine := newWaitTestMachine(t, tc.status)
			machine.WaitForStateChange(20)

			if machine.Status() != tc.wantstatus {
				t.Errorf("status is %q, expected %q", machine.Status(), tc.wantstatus)
			}
			if machine.savedipaddress != "10.0.0.5" {
				t.Errorf("IP address is %q", machine.savedipaddress)
			}
			if machine.Name() != "node1" || machine.clustername != "cluster1" {
				t.Errorf("the machine is now %q in cluster %q", machine.Name(), machine.clustername)
			}
		})
	}
}

// If the wait does not work, the machine stays as it was, whatever the reason.
func TestWaitForStateChangeLeavesTheMachineWhenTheWaitFails(t *testing.T) {
	for _, how := range []string{"unsuccessful", "garbage", "exit"} {
		t.Run(how, func(t *testing.T) {
			testStorage(t)
			t.Setenv("FAKE_PS_WAITMACHINE", how)

			machine := newWaitTestMachine(t, MachineStatusStopping)
			machine.WaitForStateChange(20)

			if machine.Status() != MachineStatusStopping {
				t.Errorf("status is %q", machine.Status())
			}
			if machine.savedipaddress != "" {
				t.Errorf("IP address is %q", machine.savedipaddress)
			}
		})
	}
}
