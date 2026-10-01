package driverhyperv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kuttiproject/drivercore"
	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/workspace"
)

// ImagesVersion defines the image repository version for the current version
// of the driver.
const ImagesVersion = "0.2"

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
		return errors.New("file is not valid")
	}

	localfilepath, err := imagepathfromk8sversion(k8sversion)
	if err != nil {
		return err
	}

	// If a cached copy of this image already exists, machines may already
	// have differencing disks backed by it. Overwriting it in place with
	// different content would silently corrupt every one of those disks.
	if existingchecksum, chkerr := workspace.ChecksumFile(localfilepath); chkerr == nil {
		if existingchecksum == checksum {
			// Already have this exact file cached; nothing to do.
			return nil
		}

		inuse, useerr := isImageInUse(k8sversion)
		if useerr != nil {
			return fmt.Errorf(
				"cannot replace cached image for K8s version %s: could not verify it is unused: %v",
				k8sversion, useerr,
			)
		}
		if inuse {
			return fmt.Errorf(
				"cannot replace cached image for K8s version %s: it is in use as a backing disk by one or more machines",
				k8sversion,
			)
		}
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
	kuttilog.Printf(kuttilog.Info, "Purging cached image for Kubernetes %s...", k8sversion)
	inuse, err := isImageInUse(k8sversion)
	if err != nil {
		return fmt.Errorf(
			"cannot remove cached image for K8s version %s: could not verify it is unused: %v",
			k8sversion, err,
		)
	}
	if inuse {
		return fmt.Errorf(
			"cannot remove cached image for K8s version %s: it is in use as a backing disk by one or more machines",
			k8sversion,
		)
	}

	filename, err := imagepathfromk8sversion(k8sversion)
	if err != nil {
		return err
	}
	kuttilog.Printf(kuttilog.Debug, "Deleting image file: %s", filename)
	return workspace.RemoveFile(filename)
}

// isImageInUse reports whether the cached master image for k8sversion is
// currently set as the parent/backing file of any differencing disk in the VM
// disks directory. It is used to prevent removing or overwriting a master
// image out from under machines that depend on it.
func isImageInUse(k8sversion string) (bool, error) {
	imagePath, err := imagepathfromk8sversion(k8sversion)
	if err != nil {
		return false, err
	}
	imagePath, err = filepath.Abs(imagePath)
	if err != nil {
		return false, err
	}
	kuttilog.Printf(kuttilog.Debug, "Checking if image %s is in use by any differencing disks", imagePath)

	disksDir, err := diskDir()
	if err != nil {
		return false, err
	}

	entries, err := os.ReadDir(disksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	d := &Driver{}
	if !d.validate() {
		return false, fmt.Errorf("could not validate driver to inspect disks: %s", d.errormessage)
	}

	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".vhdx" {
			continue
		}

		diskPath := filepath.Join(disksDir, entry.Name())
		diskPath, err = filepath.Abs(diskPath)
		if err != nil {
			return false, fmt.Errorf("could not resolve absolute path for disk %s: %v", entry.Name(), err)
		}

		res, err := d.runwithresults("getvhdparent", diskPath)
		if err != nil {
			return false, fmt.Errorf("could not inspect disk %s: %v", diskPath, err)
		}
		if !res.Success {
			return false, fmt.Errorf("could not inspect disk %s: %s", diskPath, res.ErrorMessage)
		}

		payload, ok := res.Payload["ParentPath"].(string)
		if ok && payload != "" {
			parentAbs, err := filepath.Abs(payload)
			if err != nil {
				return false, fmt.Errorf("could not resolve parent path for %s: %v", diskPath, err)
			}
			if strings.EqualFold(parentAbs, imagePath) {
				kuttilog.Printf(kuttilog.Debug, "Image %s is in use by disk %s", imagePath, diskPath)
				return true, nil
			}
		}
	}

	kuttilog.Printf(kuttilog.Debug, "Image %s is not in use by any differencing disks", imagePath)
	return false, nil
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
