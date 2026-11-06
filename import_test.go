package driverhyperv

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// importProbeEnv makes TestMain exit as soon as the package has been
// imported.
const importProbeEnv = "KUTTI_TEST_IMPORT_PROBE"

// Importing the package reads and writes nothing, and makes no directories.
// This is checked in a process that only imports the package, with every
// place a user's files could go pointed at an empty directory. The
// directories that hold the user's config and cache directories exist, as
// they do for a real user, because the workspace makes only the last
// directory of a path.
func TestImportingThePackageDoesNothing(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	places := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
		"APPDATA":         filepath.Join(home, "appdata"),
		"LOCALAPPDATA":    filepath.Join(home, "localappdata"),
	}

	env := []string{importProbeEnv + "=1", "HOME=" + home, "USERPROFILE=" + home}
	for name, dir := range places {
		if err := os.Mkdir(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		env = append(env, name+"="+dir)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the process failed: %v\n%s", err, out)
	}

	filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if path == home {
			return nil
		}
		for _, dir := range places {
			if path == dir {
				return nil
			}
		}

		rel, _ := filepath.Rel(home, path)
		t.Errorf("importing the package made %s", rel)
		return nil
	})
}
