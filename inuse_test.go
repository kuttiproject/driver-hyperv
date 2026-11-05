package driverhyperv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInUseBackedByImage(t *testing.T) {
	root := testStorage(t)
	addDisk(t, root, "node.vhdx", cachedImagePath(root, "1.37"))

	inuse, err := isImageInUse("1.37")
	if err != nil || !inuse {
		t.Errorf("got %v, %v, expected true", inuse, err)
	}
}

func TestInUseBackedByOtherVersion(t *testing.T) {
	root := testStorage(t)
	addDisk(t, root, "node.vhdx", cachedImagePath(root, "1.36"))

	inuse, err := isImageInUse("1.37")
	if err != nil || inuse {
		t.Errorf("got %v, %v, expected false", inuse, err)
	}
}

// The comparison is of whole paths, not of beginnings.
func TestInUseNeedsTheSamePath(t *testing.T) {
	root := testStorage(t)
	addDisk(t, root, "node.vhdx", cachedImagePath(root, "1.37"))

	for _, version := range []string{"1.3", "1.37.1"} {
		inuse, err := isImageInUse(version)
		if err != nil || inuse {
			t.Errorf("version %s: got %v, %v, expected false", version, inuse, err)
		}
	}

	addDisk(t, root, "other.vhdx", cachedImagePath(root, "1.37")+".old")
	addDisk(t, root, "third.vhdx", filepath.Join(cacheDirPath(root), "other", "kutti-1.37.vhdx"))
	os.Remove(filepath.Join(disksDirPath(root), "node.vhdx.parent"))

	inuse, err := isImageInUse("1.37")
	if err != nil || inuse {
		t.Errorf("got %v, %v, expected false", inuse, err)
	}
}

// Paths on Windows do not differ by case.
func TestInUseComparesPathsIgnoringCase(t *testing.T) {
	root := testStorage(t)
	addDisk(t, root, "node.vhdx", strings.ToUpper(cachedImagePath(root, "1.37")))

	inuse, err := isImageInUse("1.37")
	if err != nil || !inuse {
		t.Errorf("got %v, %v, expected true", inuse, err)
	}
}

func TestInUseDiskWithoutParent(t *testing.T) {
	root := testStorage(t)
	addDisk(t, root, "node.vhdx", "")

	inuse, err := isImageInUse("1.37")
	if err != nil || inuse {
		t.Errorf("got %v, %v, expected false", inuse, err)
	}
}

func TestInUseDiskWithoutPayload(t *testing.T) {
	root := testStorage(t)
	disk := addDisk(t, root, "node.vhdx", "")
	touch(t, disk+".payloadless")

	inuse, err := isImageInUse("1.37")
	if err != nil || inuse {
		t.Errorf("got %v, %v, expected false", inuse, err)
	}
}

func TestInUseNoDisks(t *testing.T) {
	testStorage(t)

	inuse, err := isImageInUse("1.37")
	if err != nil || inuse {
		t.Errorf("got %v, %v, expected false", inuse, err)
	}
}

func TestInUseOneDiskOfSeveral(t *testing.T) {
	root := testStorage(t)
	addDisk(t, root, "a.vhdx", cachedImagePath(root, "1.36"))
	addDisk(t, root, "b.vhdx", cachedImagePath(root, "1.37"))
	addDisk(t, root, "c.vhdx", "")

	inuse, err := isImageInUse("1.37")
	if err != nil || !inuse {
		t.Errorf("1.37: got %v, %v, expected true", inuse, err)
	}

	inuse, err = isImageInUse("1.38")
	if err != nil || inuse {
		t.Errorf("1.38: got %v, %v, expected false", inuse, err)
	}
}

// Only files that end in .vhdx, in any case, are VM disks.
func TestInUseOnlyLooksAtVHDXFiles(t *testing.T) {
	root := testStorage(t)
	parent := cachedImagePath(root, "1.37")

	addDisk(t, root, "other.vhd", parent)
	addDisk(t, root, "other.img", parent)
	addDisk(t, root, "vhdx", parent)

	dir := filepath.Join(disksDirPath(root), "dir.vhdx")
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+".parent", []byte(parent), 0o666); err != nil {
		t.Fatal(err)
	}

	inuse, err := isImageInUse("1.37")
	if err != nil || inuse {
		t.Fatalf("got %v, %v, expected false", inuse, err)
	}

	addDisk(t, root, "NODE.VHDX", parent)

	inuse, err = isImageInUse("1.37")
	if err != nil || !inuse {
		t.Errorf("with a disk in capitals: got %v, %v, expected true", inuse, err)
	}
}

// If a disk cannot be checked, the image cannot be shown to be unused.
func TestInUseFailsClosedWhenDiskCannotBeInspected(t *testing.T) {
	root := testStorage(t)
	disk := addDisk(t, root, "node.vhdx", "")
	touch(t, disk+".fail")

	inuse, err := isImageInUse("1.37")
	if err == nil || inuse {
		t.Fatalf("got %v, %v, expected an error", inuse, err)
	}
	if !strings.Contains(err.Error(), "could not inspect disk") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInUseFailsClosedWhenPowerShellReportsFailure(t *testing.T) {
	root := testStorage(t)
	disk := addDisk(t, root, "node.vhdx", "")
	touch(t, disk+".unsuccessful")

	inuse, err := isImageInUse("1.37")
	if err == nil || inuse {
		t.Fatalf("got %v, %v, expected an error", inuse, err)
	}
	if !strings.Contains(err.Error(), "could not inspect disk") || !strings.Contains(err.Error(), "The file is in use") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInUseFailsClosedOnOutputThatIsNotJSON(t *testing.T) {
	root := testStorage(t)
	disk := addDisk(t, root, "node.vhdx", "")
	touch(t, disk+".garbage")

	inuse, err := isImageInUse("1.37")
	if err == nil || inuse {
		t.Fatalf("got %v, %v, expected an error", inuse, err)
	}
	if !strings.Contains(err.Error(), "could not inspect disk") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Even with no disks to look at, the driver has to be usable.
func TestInUseNeedsAUsableDriver(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"no PowerShell":    func(t *testing.T) { noPowerShell(t) },
		"Hyper-V disabled": func(t *testing.T) { t.Setenv("FAKE_PS_CHECKDRIVER", "unavailable") },
	}

	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			root := testStorage(t)
			addDisk(t, root, "node.vhdx", "")
			spoil(t)

			inuse, err := isImageInUse("1.37")
			if err == nil || inuse {
				t.Fatalf("got %v, %v, expected an error", inuse, err)
			}
			if !strings.Contains(err.Error(), "could not validate driver") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestInUseNeedsAUsableDriverEvenWithNoDisks(t *testing.T) {
	testStorage(t)
	t.Setenv("FAKE_PS_CHECKDRIVER", "unavailable")

	inuse, err := isImageInUse("1.37")
	if err == nil || inuse {
		t.Fatalf("got %v, %v, expected an error", inuse, err)
	}
	if !strings.Contains(err.Error(), "Hyper-V not enabled") {
		t.Errorf("the error does not say why: %v", err)
	}
}

// The driver is checked, and then every disk is asked for its parent, with
// its absolute path.
func TestInUseAsksPowerShellAboutEveryDisk(t *testing.T) {
	root := testStorage(t)
	a := addDisk(t, root, "a.vhdx", "")
	b := addDisk(t, root, "b.vhdx", "")

	if _, err := isImageInUse("1.37"); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(cacheDirPath(root), "hypervmanage-0.3.ps1")
	prefix := "-NoProfile -NonInteractive -File " + script + " "
	expected := []string{
		prefix + "checkdriver",
		prefix + "getvhdparent " + a,
		prefix + "getvhdparent " + b,
	}

	calls := psCalls(t)
	if strings.Join(calls, "\n") != strings.Join(expected, "\n") {
		t.Errorf("PowerShell was called as\n%s\nexpected\n%s", strings.Join(calls, "\n"), strings.Join(expected, "\n"))
	}
}
