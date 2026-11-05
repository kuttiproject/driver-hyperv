package driverhyperv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultImagesVersionAndSourceURL(t *testing.T) {
	if ImagesVersion != "0.2" {
		t.Errorf("images version is %s", ImagesVersion)
	}

	expected := "https://github.com/kuttiproject/driver-hyperv-images/releases/download/v0.2/driver-hyperv-images.json"
	if ImagesSourceURL != expected {
		t.Errorf("source URL is %s, expected %s", ImagesSourceURL, expected)
	}
}

func TestStorageDirectories(t *testing.T) {
	root := testStorage(t)

	cases := []struct {
		name string
		get  func() (string, error)
		want string
	}{
		{"images", hypervCacheDir, cacheDirPath(root)},
		{"disks", diskDir, disksDirPath(root)},
		{"machines", machineDir, machinesDirPath(root)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.get()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %s, expected %s", got, tc.want)
			}

			// The directory is made when it is asked for.
			info, err := os.Stat(got)
			if err != nil || !info.IsDir() {
				t.Errorf("%s is not a directory: %v", got, err)
			}
		})
	}
}

// The interface script is kept next to the cached images, under a name that
// has the version of the script in it.
func TestScriptLocationAndContent(t *testing.T) {
	root := testStorage(t)

	if scriptname != "hypervmanage-0.3.ps1" {
		t.Errorf("script name is %s", scriptname)
	}

	scriptpath, err := findScript()
	if err != nil {
		t.Fatal(err)
	}
	if scriptpath != filepath.Join(cacheDirPath(root), "hypervmanage-0.3.ps1") {
		t.Errorf("script path is %s", scriptpath)
	}

	data, err := os.ReadFile(scriptpath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != script || len(script) == 0 {
		t.Error("the script file does not hold the embedded script")
	}
}

// A script file that exists is used as it is, and not written again.
func TestScriptThatExistsIsNotWrittenAgain(t *testing.T) {
	testStorage(t)

	scriptpath, err := findScript()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, scriptpath, []byte("changed"))

	if _, err := findScript(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, scriptpath); string(got) != "changed" {
		t.Errorf("the script was written again: %q", got)
	}
}
