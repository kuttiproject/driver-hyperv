package driverhyperv

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kuttiproject/drivercore"
	"github.com/kuttiproject/workspace"
)

// These tests use only what a user of the driver can use: the Driver methods
// that deal with images, and the Image methods. They check what the driver
// does with a list and image files from a local server, with the storage in
// temporary directories and a fake PowerShell.
//
// The images of this driver are .vhdx files, which are published, and
// downloaded, in zip files with the .vhdx file as the only entry. The
// checksum in the image list is that of the .vhdx file.

type testimage struct {
	version    string
	content    []byte
	deprecated bool

	// path, if set, is where the server serves the file of the image, and
	// so what its source URL is. Otherwise it is "/kutti-<version>.vhdx.zip".
	path string
}

func (ti testimage) urlpath() string {
	if ti.path != "" {
		return ti.path
	}

	return "/kutti-" + ti.version + ".vhdx.zip"
}

func (ti testimage) checksum() string {
	return fmt.Sprintf("%x", sha256.Sum256(ti.content))
}

// zipped returns the zip file of the image.
func (ti testimage) zipped(t *testing.T) []byte {
	return makeZip(t, zipentry{"kutti-" + ti.version + ".vhdx", ti.content})
}

type zipentry struct {
	name string
	data []byte
}

func makeZip(t *testing.T, entries ...zipentry) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		f, err := w.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// content makes content for a test image that is different for different
// seeds.
func content(seed string, size int) []byte {
	result := make([]byte, size)
	for i := range result {
		result[i] = seed[i%len(seed)] + byte(i%251)
	}

	return result
}

// imageserver serves an image list, and the images in it. A test can change
// what it serves.
type imageserver struct {
	*httptest.Server

	mu       sync.Mutex
	images   []testimage
	listcode int
	files    map[string][]byte
}

func newImageServer(t *testing.T, images ...testimage) *imageserver {
	t.Helper()

	is := &imageserver{listcode: http.StatusOK, files: map[string][]byte{}}
	is.set(t, images...)

	is.Server = httptest.NewServer(http.HandlerFunc(is.handle))
	t.Cleanup(is.Close)

	saved := ImagesSourceURL
	ImagesSourceURL = is.URL + "/" + imagesConfigFile
	t.Cleanup(func() { ImagesSourceURL = saved })

	return is
}

// set changes the images in the list. The files served are the zip files of
// the images.
func (is *imageserver) set(t *testing.T, images ...testimage) {
	t.Helper()

	is.mu.Lock()
	defer is.mu.Unlock()

	is.images = images
	for _, img := range images {
		is.files[img.urlpath()] = img.zipped(t)
	}
}

// serveFile changes what is served as the file of an image, without changing
// the list.
func (is *imageserver) serveFile(version string, data []byte) {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.files["/kutti-"+version+".vhdx.zip"] = data
}

func (is *imageserver) failList(code int) {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.listcode = code
}

func (is *imageserver) handle(w http.ResponseWriter, r *http.Request) {
	is.mu.Lock()
	defer is.mu.Unlock()

	if r.URL.Path == "/"+imagesConfigFile {
		if is.listcode != http.StatusOK {
			http.Error(w, "no list", is.listcode)
			return
		}

		list := map[string]map[string]interface{}{}
		for _, img := range is.images {
			list[img.version] = map[string]interface{}{
				"ImageK8sVersion": img.version,
				"ImageChecksum":   img.checksum(),
				"ImageSourceURL":  "http://" + r.Host + img.urlpath(),
				"ImageStatus":     "NotDownloaded",
				"ImageDeprecated": img.deprecated,
			}
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	data, ok := is.files[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.Write(data)
}

func getImage(t *testing.T, version string) drivercore.Image {
	t.Helper()

	img, err := (&Driver{}).GetImage(version)
	if err != nil {
		t.Fatal(err)
	}

	return img
}

func isDownloaded(t *testing.T, version string) bool {
	t.Helper()
	return getImage(t, version).Status() == drivercore.ImageStatusDownloaded
}

// updateList gets the list from the server.
func updateList(t *testing.T) {
	t.Helper()

	if err := (&Driver{}).UpdateImageList(); err != nil {
		t.Fatalf("UpdateImageList failed: %v", err)
	}
}

// statusInListFile reads the status of an image from the saved list.
func statusInListFile(t *testing.T, version string) string {
	t.Helper()

	dir, err := workspace.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, imagesConfigFile))
	if err != nil {
		t.Fatal(err)
	}

	var list map[string]map[string]interface{}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}

	status, _ := list[version]["ImageStatus"].(string)
	return status
}

// imageFiles returns the sorted names of the entries of the cache directory,
// except for the interface script, which is kept there too. A cache directory
// that does not exist yet holds nothing.
func imageFiles(t *testing.T, root string) []string {
	t.Helper()

	entries, err := os.ReadDir(cacheDirPath(root))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	names := []string{}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "hypervmanage-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	return names
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestImageListQueries(t *testing.T) {
	testStorage(t)
	newImageServer(t,
		testimage{version: "1.36", content: content("a", 100), deprecated: true},
		testimage{version: "1.37", content: content("b", 100), deprecated: false},
	)
	updateList(t)

	d := &Driver{}

	versions := d.K8sVersions()
	sort.Strings(versions)
	if strings.Join(versions, ",") != "1.36,1.37" {
		t.Errorf("versions are %v", versions)
	}

	if !d.ValidK8sVersion("1.37") || d.ValidK8sVersion("1.99") {
		t.Error("ValidK8sVersion gave the wrong answer")
	}

	images, err := d.ListImages()
	if err != nil || len(images) != 2 {
		t.Fatalf("ListImages returned %d images, %v", len(images), err)
	}

	old := getImage(t, "1.36")
	if old.K8sVersion() != "1.36" || !old.Deprecated() {
		t.Errorf("image 1.36 is %q, deprecated %v", old.K8sVersion(), old.Deprecated())
	}
	current := getImage(t, "1.37")
	if current.K8sVersion() != "1.37" || current.Deprecated() {
		t.Errorf("image 1.37 is %q, deprecated %v", current.K8sVersion(), current.Deprecated())
	}
	if current.Status() == drivercore.ImageStatusDownloaded {
		t.Error("a new image is marked as downloaded")
	}

	_, err = d.GetImage("1.99")
	if err == nil {
		t.Fatal("GetImage of an unknown version succeeded")
	}
	if !strings.Contains(err.Error(), "1.99") {
		t.Errorf("the error does not name the version: %v", err)
	}
}

func TestNoImagesBeforeTheListIsUpdated(t *testing.T) {
	testStorage(t)
	d := &Driver{}

	if versions := d.K8sVersions(); len(versions) != 0 {
		t.Errorf("versions are %v", versions)
	}
	if d.ValidK8sVersion("1.37") {
		t.Error("a version is valid before the list is updated")
	}
	if images, err := d.ListImages(); err != nil || len(images) != 0 {
		t.Errorf("ListImages returned %d images, %v", len(images), err)
	}
}

func TestUpdateImageListFailureKeepsList(t *testing.T) {
	testStorage(t)
	server := newImageServer(t, testimage{version: "1.37", content: content("b", 100), deprecated: false})
	updateList(t)

	server.failList(http.StatusNotFound)
	if err := (&Driver{}).UpdateImageList(); err == nil {
		t.Error("UpdateImageList succeeded although the server failed")
	}

	if versions := (&Driver{}).K8sVersions(); len(versions) != 1 || versions[0] != "1.37" {
		t.Errorf("versions are %v", versions)
	}
}

func TestFetch(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 3<<20), deprecated: false}
	newImageServer(t, image)
	updateList(t)

	if err := getImage(t, "1.37").Fetch(); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if string(readFile(t, cachedImagePath(root, "1.37"))) != string(image.content) {
		t.Error("the cached file does not hold the image")
	}
	if names := imageFiles(t, root); strings.Join(names, ",") != "kutti-1.37.vhdx" {
		t.Errorf("the cache directory holds %v", names)
	}

	if !isDownloaded(t, "1.37") {
		t.Error("the image is not marked as downloaded")
	}
	if status := statusInListFile(t, "1.37"); status != string(drivercore.ImageStatusDownloaded) {
		t.Errorf("the saved list has status %q", status)
	}
}

func TestFetchWithProgress(t *testing.T) {
	testStorage(t)
	image := testimage{version: "1.37", content: content("b", 3<<20), deprecated: false}
	server := newImageServer(t, image)
	updateList(t)

	var calls int
	var last, total int64
	err := getImage(t, "1.37").FetchWithProgress(func(current int64, size int64) {
		calls++
		last, total = current, size
	})
	if err != nil {
		t.Fatal(err)
	}

	// The progress is that of the download, which is of the zip file.
	size := int64(len(server.files["/kutti-1.37.vhdx.zip"]))
	if calls == 0 || last != size || total != last {
		t.Errorf("progress was called %d times, ending at %d of %d, the zip file is %d bytes", calls, last, total, size)
	}
}

func TestFetchOfCorruptImageKeepsNothing(t *testing.T) {
	root := testStorage(t)
	server := newImageServer(t, testimage{version: "1.37", content: content("b", 1000), deprecated: false})
	updateList(t)

	server.serveFile("1.37", testimage{version: "1.37", content: content("x", 1000)}.zipped(t))

	err := getImage(t, "1.37").Fetch()
	if err == nil || !strings.Contains(err.Error(), "file is not valid") {
		t.Fatalf("Fetch returned %v, expected an error that the file is not valid", err)
	}

	if names := imageFiles(t, root); len(names) != 0 {
		t.Errorf("the cache directory holds %v", names)
	}
	if isDownloaded(t, "1.37") {
		t.Error("the image is marked as downloaded")
	}
}

// Whatever is wrong with the download, nothing is kept.
func TestFetchOfDownloadThatIsNotAnImageKeepsNothing(t *testing.T) {
	good := content("b", 1000)
	cases := []struct {
		name string
		data []byte
		want func(err error) bool
	}{
		{"not a zip file", []byte("this is not a zip file"), func(err error) bool { return errors.Is(err, zip.ErrFormat) }},
		{"a zip file with no entries", nil, func(err error) bool { return err.Error() == "invalid compressed image" }},
		{"a zip file with two entries", nil, func(err error) bool { return err.Error() == "invalid compressed image" }},
	}

	for i := range cases {
		switch cases[i].name {
		case "a zip file with no entries":
			cases[i].data = makeZip(t)
		case "a zip file with two entries":
			cases[i].data = makeZip(t, zipentry{"a.vhdx", good}, zipentry{"b.vhdx", good})
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := testStorage(t)
			server := newImageServer(t, testimage{version: "1.37", content: good, deprecated: false})
			updateList(t)
			server.serveFile("1.37", tc.data)

			err := getImage(t, "1.37").Fetch()
			if err == nil || !tc.want(err) {
				t.Fatalf("Fetch returned %v", err)
			}
			if names := imageFiles(t, root); len(names) != 0 {
				t.Errorf("the cache directory holds %v", names)
			}
			if isDownloaded(t, "1.37") {
				t.Error("the image is marked as downloaded")
			}
		})
	}
}

func TestFetchWhenTheServerFailsKeepsNothing(t *testing.T) {
	root := testStorage(t)
	server := newImageServer(t, testimage{version: "1.37", content: content("b", 1000), deprecated: false})
	updateList(t)

	server.mu.Lock()
	delete(server.files, "/kutti-1.37.vhdx.zip")
	server.mu.Unlock()

	if err := getImage(t, "1.37").Fetch(); err == nil {
		t.Fatal("Fetch succeeded although the server has no file")
	}
	if names := imageFiles(t, root); len(names) != 0 {
		t.Errorf("the cache directory holds %v", names)
	}
	if isDownloaded(t, "1.37") {
		t.Error("the image is marked as downloaded")
	}
}

func TestFromFileOfVHDXFile(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
	newImageServer(t, image)
	updateList(t)

	source := filepath.Join(t.TempDir(), "mine.vhdx")
	writeFile(t, source, image.content)

	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatalf("FromFile failed: %v", err)
	}

	if string(readFile(t, cachedImagePath(root, "1.37"))) != string(image.content) {
		t.Error("the cached file does not hold the image")
	}
	if !isDownloaded(t, "1.37") {
		t.Error("the image is not marked as downloaded")
	}
	if names := imageFiles(t, root); strings.Join(names, ",") != "kutti-1.37.vhdx" {
		t.Errorf("the cache directory holds %v", names)
	}

	// The file of the user is theirs, and stays.
	if string(readFile(t, source)) != string(image.content) {
		t.Error("the source file was changed or removed")
	}
}

func TestFromFileOfZipFile(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
	newImageServer(t, image)
	updateList(t)

	source := filepath.Join(t.TempDir(), "mine.zip")
	writeFile(t, source, image.zipped(t))

	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatalf("FromFile failed: %v", err)
	}

	if string(readFile(t, cachedImagePath(root, "1.37"))) != string(image.content) {
		t.Error("the cached file does not hold the image")
	}
	if !isDownloaded(t, "1.37") {
		t.Error("the image is not marked as downloaded")
	}
	if names := imageFiles(t, root); strings.Join(names, ",") != "kutti-1.37.vhdx" {
		t.Errorf("the cache directory holds %v", names)
	}
	if string(readFile(t, source)) != string(image.zipped(t)) {
		t.Error("the source file was changed or removed")
	}
}

// What the entry in the zip file is called does not matter, and it is never
// used as a path.
func TestFromFileOfZipFileIgnoresTheNameOfTheEntry(t *testing.T) {
	for _, name := range []string{"whatever.bin", "../escape.vhdx", "sub/dir/disk.vhdx", "kutti-1.37.vhdx"} {
		t.Run(name, func(t *testing.T) {
			root := testStorage(t)
			image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
			newImageServer(t, image)
			updateList(t)

			source := filepath.Join(t.TempDir(), "mine.zip")
			writeFile(t, source, makeZip(t, zipentry{name, image.content}))

			if err := getImage(t, "1.37").FromFile(source); err != nil {
				t.Fatalf("FromFile failed: %v", err)
			}
			if string(readFile(t, cachedImagePath(root, "1.37"))) != string(image.content) {
				t.Error("the cached file does not hold the image")
			}

			// Nothing was made anywhere else in the workspace.
			if names := imageFiles(t, root); strings.Join(names, ",") != "kutti-1.37.vhdx" {
				t.Errorf("the cache directory holds %v", names)
			}
			for _, dir := range []string{root, filepath.Dir(cacheDirPath(root))} {
				entries, _ := os.ReadDir(dir)
				for _, e := range entries {
					if e.Name() == "escape.vhdx" || e.Name() == "sub" {
						t.Errorf("%s holds %s", dir, e.Name())
					}
				}
			}
		})
	}
}

func TestFromFileAcceptsAnyCaseOfExtension(t *testing.T) {
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}

	for _, name := range []string{"mine.VHDX", "mine.Vhdx", "mine.ZIP", "mine.Zip"} {
		t.Run(name, func(t *testing.T) {
			root := testStorage(t)
			newImageServer(t, image)
			updateList(t)

			source := filepath.Join(t.TempDir(), name)
			if strings.HasSuffix(strings.ToLower(name), ".zip") {
				writeFile(t, source, image.zipped(t))
			} else {
				writeFile(t, source, image.content)
			}

			if err := getImage(t, "1.37").FromFile(source); err != nil {
				t.Fatalf("FromFile failed: %v", err)
			}
			if string(readFile(t, cachedImagePath(root, "1.37"))) != string(image.content) {
				t.Error("the cached file does not hold the image")
			}
		})
	}
}

func TestFromFileRejectsOtherFileTypes(t *testing.T) {
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}

	for _, name := range []string{"mine.img", "mine.vhd", "mine.qcow2", "mine.vhdx.bak", "mine", "mine.zip.txt"} {
		t.Run(name, func(t *testing.T) {
			root := testStorage(t)
			newImageServer(t, image)
			updateList(t)

			source := filepath.Join(t.TempDir(), name)
			writeFile(t, source, image.content)

			err := getImage(t, "1.37").FromFile(source)
			if err == nil || err.Error() != "only .vhdx or .zip files allowed" {
				t.Fatalf("FromFile returned %v", err)
			}
			if names := imageFiles(t, root); len(names) != 0 {
				t.Errorf("the cache directory holds %v", names)
			}
			if isDownloaded(t, "1.37") {
				t.Error("the image is marked as downloaded")
			}
		})
	}
}

func TestFromFileRejectsWrongFile(t *testing.T) {
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
	other := content("x", 5000)

	cases := map[string][]byte{
		"mine.vhdx": other,
		"mine.zip":  makeZip(t, zipentry{"mine.vhdx", other}),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			root := testStorage(t)
			newImageServer(t, image)
			updateList(t)

			source := filepath.Join(t.TempDir(), name)
			writeFile(t, source, data)

			err := getImage(t, "1.37").FromFile(source)
			if err == nil || !strings.Contains(err.Error(), "file is not valid") {
				t.Errorf("FromFile returned %v, expected an error that the file is not valid", err)
			}
			if _, statErr := os.Stat(cachedImagePath(root, "1.37")); statErr == nil {
				t.Error("a file was cached")
			}
			if names := imageFiles(t, root); len(names) != 0 {
				t.Errorf("the cache directory holds %v", names)
			}
			if isDownloaded(t, "1.37") {
				t.Error("the image is marked as downloaded")
			}
			if string(readFile(t, source)) != string(data) {
				t.Error("the source file was changed or removed")
			}
		})
	}
}

func TestFromFileOfFilesThatCannotBeUsed(t *testing.T) {
	root := testStorage(t)
	newImageServer(t, testimage{version: "1.37", content: content("b", 5000), deprecated: false})
	updateList(t)

	dir := t.TempDir()
	notzip := filepath.Join(dir, "notzip.zip")
	writeFile(t, notzip, []byte("this is not a zip file"))
	twoentries := filepath.Join(dir, "two.zip")
	writeFile(t, twoentries, makeZip(t, zipentry{"a", []byte("a")}, zipentry{"b", []byte("b")}))
	noentries := filepath.Join(dir, "none.zip")
	writeFile(t, noentries, makeZip(t))

	for _, source := range []string{
		filepath.Join(dir, "missing.vhdx"),
		filepath.Join(dir, "missing.zip"),
		notzip, twoentries, noentries,
	} {
		if err := getImage(t, "1.37").FromFile(source); err == nil {
			t.Errorf("FromFile of %s succeeded", filepath.Base(source))
		}
	}

	if names := imageFiles(t, root); len(names) != 0 {
		t.Errorf("the cache directory holds %v", names)
	}
	if isDownloaded(t, "1.37") {
		t.Error("the image is marked as downloaded")
	}
}

// Importing what is cached already does not copy it again.
func TestFromFileOfCachedContentDoesNotCopy(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
	newImageServer(t, image)
	updateList(t)

	source := filepath.Join(t.TempDir(), "mine.vhdx")
	writeFile(t, source, image.content)
	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatal(err)
	}

	cached := cachedImagePath(root, "1.37")
	longago := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(cached, longago, longago); err != nil {
		t.Fatal(err)
	}

	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatalf("second FromFile failed: %v", err)
	}

	info, err := os.Stat(cached)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(longago) {
		t.Error("the cached file was written again")
	}
}

// cacheOldAndPublishNew caches an image, and then changes the list so that
// the image has other content. It returns a file with the new content.
func cacheOldAndPublishNew(t *testing.T) (oldcontent []byte, newfile string, newcontent []byte) {
	t.Helper()

	oldimage := testimage{version: "1.37", content: content("old", 5000), deprecated: false}
	newimage := testimage{version: "1.37", content: content("new", 6000), deprecated: false}
	server := newImageServer(t, oldimage)
	updateList(t)

	source := filepath.Join(t.TempDir(), "old.vhdx")
	writeFile(t, source, oldimage.content)
	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatal(err)
	}

	server.set(t, newimage)
	updateList(t)

	newfile = filepath.Join(t.TempDir(), "new.vhdx")
	writeFile(t, newfile, newimage.content)

	return oldimage.content, newfile, newimage.content
}

// A cached image that machines are using must not change under them.
func TestFromFileDoesNotReplaceImageInUse(t *testing.T) {
	root := testStorage(t)
	oldcontent, newfile, _ := cacheOldAndPublishNew(t)

	addDisk(t, root, "node.vhdx", cachedImagePath(root, "1.37"))

	err := getImage(t, "1.37").FromFile(newfile)
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("FromFile returned %v, expected an error that the image is in use", err)
	}
	if string(readFile(t, cachedImagePath(root, "1.37"))) != string(oldcontent) {
		t.Error("the cached image was changed")
	}
}

// The same goes for an image that arrives in a zip file.
func TestFromFileOfZipFileDoesNotReplaceImageInUse(t *testing.T) {
	root := testStorage(t)
	oldcontent, newfile, newcontent := cacheOldAndPublishNew(t)

	addDisk(t, root, "node.vhdx", cachedImagePath(root, "1.37"))

	zipfile := filepath.Join(t.TempDir(), "new.zip")
	writeFile(t, zipfile, makeZip(t, zipentry{filepath.Base(newfile), newcontent}))

	err := getImage(t, "1.37").FromFile(zipfile)
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("FromFile returned %v, expected an error that the image is in use", err)
	}
	if string(readFile(t, cachedImagePath(root, "1.37"))) != string(oldcontent) {
		t.Error("the cached image was changed")
	}
	if names := imageFiles(t, root); strings.Join(names, ",") != "kutti-1.37.vhdx" {
		t.Errorf("the cache directory holds %v", names)
	}
}

func TestFromFileDoesNotReplaceImageThatCannotBeShownToBeUnused(t *testing.T) {
	root := testStorage(t)
	oldcontent, newfile, _ := cacheOldAndPublishNew(t)

	disk := addDisk(t, root, "node.vhdx", "")
	touch(t, disk+".fail")

	err := getImage(t, "1.37").FromFile(newfile)
	if err == nil || !strings.Contains(err.Error(), "could not verify it is unused") {
		t.Fatalf("FromFile returned %v, expected an error that it could not be verified", err)
	}
	if string(readFile(t, cachedImagePath(root, "1.37"))) != string(oldcontent) {
		t.Error("the cached image was changed")
	}
}

func TestFromFileReplacesImageNotInUse(t *testing.T) {
	root := testStorage(t)
	_, newfile, newcontent := cacheOldAndPublishNew(t)

	addDisk(t, root, "node.vhdx", cachedImagePath(root, "1.36"))

	if err := getImage(t, "1.37").FromFile(newfile); err != nil {
		t.Fatalf("FromFile failed: %v", err)
	}
	if string(readFile(t, cachedImagePath(root, "1.37"))) != string(newcontent) {
		t.Error("the cached image was not replaced")
	}
	if !isDownloaded(t, "1.37") {
		t.Error("the image is not marked as downloaded")
	}
}

func TestPurgeLocal(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
	newImageServer(t, image)
	updateList(t)

	// Purging an image that is not downloaded does nothing.
	if err := getImage(t, "1.37").PurgeLocal(); err != nil {
		t.Errorf("PurgeLocal of an image that is not downloaded failed: %v", err)
	}

	source := filepath.Join(t.TempDir(), "mine.vhdx")
	writeFile(t, source, image.content)
	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatal(err)
	}

	if err := getImage(t, "1.37").PurgeLocal(); err != nil {
		t.Fatalf("PurgeLocal failed: %v", err)
	}
	if _, err := os.Stat(cachedImagePath(root, "1.37")); err == nil {
		t.Error("the cached file is still there")
	}
	if isDownloaded(t, "1.37") {
		t.Error("the image is still marked as downloaded")
	}
	if status := statusInListFile(t, "1.37"); status == string(drivercore.ImageStatusDownloaded) {
		t.Errorf("the saved list has status %q", status)
	}
}

func TestPurgeLocalKeepsImageInUse(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
	newImageServer(t, image)
	updateList(t)

	source := filepath.Join(t.TempDir(), "mine.vhdx")
	writeFile(t, source, image.content)
	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatal(err)
	}
	addDisk(t, root, "node.vhdx", cachedImagePath(root, "1.37"))

	err := getImage(t, "1.37").PurgeLocal()
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("PurgeLocal returned %v, expected an error that the image is in use", err)
	}
	if _, statErr := os.Stat(cachedImagePath(root, "1.37")); statErr != nil {
		t.Error("the cached file was removed")
	}
	if !isDownloaded(t, "1.37") {
		t.Error("the image is no longer marked as downloaded")
	}
}

func TestPurgeLocalKeepsImageThatCannotBeShownToBeUnused(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 5000), deprecated: false}
	newImageServer(t, image)
	updateList(t)

	source := filepath.Join(t.TempDir(), "mine.vhdx")
	writeFile(t, source, image.content)
	if err := getImage(t, "1.37").FromFile(source); err != nil {
		t.Fatal(err)
	}
	disk := addDisk(t, root, "node.vhdx", "")
	touch(t, disk+".fail")

	err := getImage(t, "1.37").PurgeLocal()
	if err == nil || !strings.Contains(err.Error(), "could not verify it is unused") {
		t.Fatalf("PurgeLocal returned %v, expected an error that it could not be verified", err)
	}
	if _, statErr := os.Stat(cachedImagePath(root, "1.37")); statErr != nil {
		t.Error("the cached file was removed")
	}
	if !isDownloaded(t, "1.37") {
		t.Error("the image is no longer marked as downloaded")
	}
}

// An update keeps the downloaded status of an image only if the image has
// not changed, and never makes an image downloaded.
func TestUpdateImageListKeepsDownloadedStatusOfUnchangedImage(t *testing.T) {
	root := testStorage(t)
	image := testimage{version: "1.37", content: content("b", 5000)}
	newImageServer(t, image)
	updateList(t)

	updateList(t)
	if isDownloaded(t, "1.37") {
		t.Error("an update made an image that was not downloaded look downloaded")
	}

	if err := getImage(t, "1.37").Fetch(); err != nil {
		t.Fatal(err)
	}

	updateList(t)
	if !isDownloaded(t, "1.37") {
		t.Error("an update of an unchanged list lost the downloaded status")
	}
	if _, err := os.Stat(cachedImagePath(root, "1.37")); err != nil {
		t.Error("the cached file was removed by the update")
	}
}

func TestUpdateImageListDropsDownloadedStatusOfChangedImage(t *testing.T) {
	root := testStorage(t)
	original := testimage{version: "1.37", content: content("b", 5000)}
	server := newImageServer(t, original)
	updateList(t)
	if err := getImage(t, "1.37").Fetch(); err != nil {
		t.Fatal(err)
	}

	// Other content.
	server.set(t, testimage{version: "1.37", content: content("new", 5000)})
	updateList(t)
	if isDownloaded(t, "1.37") {
		t.Error("an image whose checksum changed is still marked as downloaded")
	}

	// The same content, from somewhere else, is another image too.
	server.set(t, original)
	updateList(t)
	if err := getImage(t, "1.37").Fetch(); err != nil {
		t.Fatal(err)
	}
	moved := original
	moved.path = "/mirror/kutti-1.37.vhdx.zip"
	server.set(t, moved)
	updateList(t)
	if isDownloaded(t, "1.37") {
		t.Error("an image whose source URL changed is still marked as downloaded")
	}

	if _, err := os.Stat(cachedImagePath(root, "1.37")); err != nil {
		t.Error("the cached file was removed by an update")
	}
}

// The image list is kept in the config directory, and read again from there.
func TestImageListIsKeptInTheConfigDirectory(t *testing.T) {
	root := testStorage(t)
	newImageServer(t, testimage{version: "1.37", content: content("b", 100)})
	updateList(t)

	if _, err := os.Stat(listFilePath(root)); err != nil {
		t.Fatalf("the list file is not where it should be: %v", err)
	}

	// A change to the file is seen.
	data := readFile(t, listFilePath(root))
	changed := strings.Replace(string(data), "1.37", "1.38", -1)
	writeFile(t, listFilePath(root), []byte(changed))

	if versions := (&Driver{}).K8sVersions(); len(versions) != 1 || versions[0] != "1.38" {
		t.Errorf("versions are %v", versions)
	}
}
