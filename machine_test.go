package driverhyperv

import (
	"strings"
	"testing"
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
