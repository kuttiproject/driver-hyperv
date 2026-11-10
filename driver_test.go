package driverhyperv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuttiproject/drivercore"
)

func TestDriverProperties(t *testing.T) {
	d := &Driver{}

	if d.Name() != "hyperv" {
		t.Errorf("name is %q", d.Name())
	}
	if d.Description() != "Kutti driver for Hyper-V" {
		t.Errorf("description is %q", d.Description())
	}
	if d.UsesPerClusterNetworking() || d.UsesNATNetworking() {
		t.Error("the driver uses neither per-cluster nor NAT networking")
	}
}

func TestDriverIsRegistered(t *testing.T) {
	d, ok := drivercore.GetDriver("hyperv")
	if !ok {
		t.Fatal("the hyperv driver is not registered")
	}
	if _, ok := d.(*Driver); !ok {
		t.Errorf("the registered driver is a %T", d)
	}
}

func TestStatusReadyWhenTheDriverChecksOut(t *testing.T) {
	testStorage(t)
	d := &Driver{}

	if status := d.Status(); status != "Ready" {
		t.Errorf("status is %q", status)
	}
	if msg := d.Error(); msg != "" {
		t.Errorf("error is %q", msg)
	}
}

// The result of a check that works is kept.
func TestValidationIsDoneOnce(t *testing.T) {
	testStorage(t)
	d := &Driver{}

	d.Status()
	_ = d.Error()
	d.Status()

	if calls := psCalls(t); len(calls) != 1 || !strings.HasSuffix(calls[0], " checkdriver") {
		t.Errorf("PowerShell was called as %q", calls)
	}
}

// PowerShell is run with the interface script, and without a profile or
// interaction.
func TestPowerShellIsRunWithTheInterfaceScript(t *testing.T) {
	root := testStorage(t)
	(&Driver{}).Status()

	script := filepath.Join(cacheDirPath(root), scriptname)
	expected := "-NoProfile -NonInteractive -File " + script + " checkdriver"

	if calls := psCalls(t); len(calls) != 1 || calls[0] != expected {
		t.Errorf("PowerShell was called as %q, expected %q", calls, expected)
	}
}

func TestStatusErrorWhenHyperVIsNotAvailable(t *testing.T) {
	testStorage(t)
	t.Setenv("FAKE_PS_CHECKDRIVER", "unavailable")
	d := &Driver{}

	if status := d.Status(); status != "Error" {
		t.Errorf("status is %q", status)
	}
	if msg := d.Error(); msg != "Hyper-V not enabled" {
		t.Errorf("error is %q", msg)
	}
}

func TestStatusErrorWhenTheCheckGivesNoJSON(t *testing.T) {
	testStorage(t)
	t.Setenv("FAKE_PS_CHECKDRIVER", "garbage")
	d := &Driver{}

	if status := d.Status(); status != "Error" {
		t.Errorf("status is %q", status)
	}
	if d.Error() == "" {
		t.Error("there is no error message")
	}
}

func TestStatusErrorWhenPowerShellFails(t *testing.T) {
	testStorage(t)
	t.Setenv("FAKE_PS_CHECKDRIVER", "exit")
	d := &Driver{}

	if status := d.Status(); status != "Error" {
		t.Errorf("status is %q", status)
	}
	if d.Error() == "" {
		t.Error("there is no error message")
	}
}

func TestStatusErrorWhenThereIsNoPowerShell(t *testing.T) {
	testStorage(t)
	noPowerShell(t)
	d := &Driver{}

	if status := d.Status(); status != "Error" {
		t.Errorf("status is %q", status)
	}
	if msg := d.Error(); msg != "PowerShell not found" {
		t.Errorf("error is %q", msg)
	}
}

// A failed check is not kept: the driver checks again when it is asked again.
func TestValidationIsRepeatedAfterAFailure(t *testing.T) {
	testStorage(t)
	t.Setenv("FAKE_PS_CHECKDRIVER", "unavailable")
	d := &Driver{}

	if d.Status() != "Error" {
		t.Fatal("expected an error")
	}

	t.Setenv("FAKE_PS_CHECKDRIVER", "ok")
	if status := d.Status(); status != "Ready" {
		t.Errorf("status is %q after the problem was fixed", status)
	}
	if msg := d.Error(); msg != "" {
		t.Errorf("error is %q", msg)
	}
}

// Windows PowerShell is preferred to the cross-platform one.
func TestFindPowerShell(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	if _, err := lookupPowerShell(); err == nil || err.Error() != "PowerShell not found" {
		t.Errorf("with nothing on the path, got %v", err)
	}

	pwsh := filepath.Join(dir, "pwsh.exe")
	writeExecutable(t, pwsh)
	if got, err := lookupPowerShell(); err != nil || got != pwsh {
		t.Errorf("with only pwsh.exe, got %q, %v", got, err)
	}

	windowsps := filepath.Join(dir, "powershell.exe")
	writeExecutable(t, windowsps)
	if got, err := lookupPowerShell(); err != nil || got != windowsps {
		t.Errorf("with both, got %q, %v", got, err)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}
