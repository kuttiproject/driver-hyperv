package driverhyperv

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/kuttiproject/drivercore"
	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/workspace"
)

// ImagesVersion defines the image repository version for the current version
// of the driver.
const ImagesVersion = "0.1"

const imagesConfigFile = "driver-hyperv-images.json"

// ImagesSourceURL is the location where the master list of images can be found
var ImagesSourceURL = "https://github.com/kuttiproject/driver-hyperv-images/releases/download/v" + ImagesVersion + "/" + imagesConfigFile

var (
	imagedata             = &imageconfigdata{}
	imageconfigmanager, _ = workspace.NewFileConfigManager(imagesConfigFile, imagedata)
)

type imageconfigdata struct {
	images map[string]*Image
}

func (icd *imageconfigdata) Serialize() ([]byte, error) {
	return json.Marshal(icd.images)
}

func (icd *imageconfigdata) Deserialize(data []byte) error {
	loaddata := make(map[string]*Image)
	err := json.Unmarshal(data, &loaddata)
	if err == nil {
		icd.images = loaddata
	}
	return err
}

func (icd *imageconfigdata) SetDefaults() {
	icd.images = defaultimages()
}

func hypervCacheDir() (string, error) {
	return workspace.CacheSubDir("driver-hyperv")
}

func hypervConfigDir() (string, error) {
	return workspace.ConfigDir()
}

func defaultimages() map[string]*Image {
	return map[string]*Image{}
}

func imagenamefromk8sversion(k8sversion string) string {
	return "kutti-" + k8sversion + ".vhdx"
}

func imagepathfromk8sversion(k8sversion string) (string, error) {
	cachedir, err := hypervCacheDir()
	if err != nil {
		return "", err
	}

	result := filepath.Join(cachedir, imagenamefromk8sversion(k8sversion))
	return result, nil
}

func addfromfile(k8sversion string, filepath string, checksum string) error {
	kuttilog.Println(kuttilog.Info, "Checking image validity...")
	filechecksum, err := workspace.ChecksumFile(filepath)
	if err != nil {
		return err
	}

	if filechecksum != checksum {
		kuttilog.Printf(kuttilog.Debug, "checksum for file %v failed.\nWanted: %v\nGot   : %v\n", filepath, checksum, filechecksum)
		return errors.New("file  is not valid")
	}

	localfilepath, err := imagepathfromk8sversion(k8sversion)
	if err != nil {
		return err
	}

	kuttilog.Println(kuttilog.Info, "Copying image to local cache...")
	// A 128KiB buffer should help
	const BUFSIZE = 131072
	err = workspace.CopyFile(filepath, localfilepath, BUFSIZE, true)
	if err != nil {
		return err
	}

	return nil
}

func removefile(k8sversion string) error {
	filename, err := imagepathfromk8sversion(k8sversion)
	if err != nil {
		return err
	}

	return workspace.RemoveFile(filename)
}

func fetchimagelist() error {
	// Download image list into temp directory
	confdir, _ := hypervConfigDir()
	tempfilename := "hypervimagesnewlist.json"
	tempfilepath := filepath.Join(confdir, tempfilename)

	kuttilog.Printf(kuttilog.Debug, "confdir: %v\ntempfilepath: %v\n", confdir, tempfilepath)

	kuttilog.Println(kuttilog.Info, "Fetching image list...")
	kuttilog.Printf(kuttilog.Debug, "Fetching from %v into %v.", ImagesSourceURL, tempfilepath)
	err := workspace.DownloadFile(ImagesSourceURL, tempfilepath)
	kuttilog.Printf(kuttilog.Debug, "Error: %v", err)
	if err != nil {
		return err
	}
	defer workspace.RemoveFile(tempfilepath)

	// Load into object
	tempimagedata := &imageconfigdata{}
	tempconfigmanager, err := workspace.NewFileConfigManager(tempfilename, tempimagedata)
	if err != nil {
		return err
	}

	err = tempconfigmanager.Load()
	if err != nil {
		return err
	}

	// Compare against current and update
	for key, newimage := range tempimagedata.images {
		oldimage := imagedata.images[key]
		if oldimage != nil &&
			newimage.imageChecksum == oldimage.imageChecksum &&
			newimage.imageSourceURL == oldimage.imageSourceURL &&
			oldimage.imageStatus == drivercore.ImageStatusDownloaded {

			newimage.imageStatus = drivercore.ImageStatusDownloaded
		}
	}

	// Make it current
	imagedata.images = tempimagedata.images

	// Save as local configuration
	imageconfigmanager.Save()

	return nil
}
