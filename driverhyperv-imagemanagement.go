package driverhyperv

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kuttiproject/imagecore"
	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/workspace"
)

// ImagesVersion defines the image repository version for the current version
// of the driver.
const ImagesVersion = "0.2"

const imagesConfigFile = "driver-hyperv-images.json"

// ImagesSourceURL is the location where the master list of images can be found
var ImagesSourceURL = "https://github.com/kuttiproject/driver-hyperv-images/releases/download/v" + ImagesVersion + "/" + imagesConfigFile

// imagestore manages the image list and the cached image files. See the
// imagecore package.
//
// It is made by init, and not in the declaration, because isImageInUse needs
// it to find the path of an image, and Go does not allow a variable to
// depend on itself in this way. Making it does no I/O.
var imagestore *imagecore.Store

func init() {
	imagestore = imagecore.New(imagecore.Config{
		DriverName:     "driver-hyperv",
		ConfigFile:     imagesConfigFile,
		ImageExtension: ".vhdx",
		DownloadSuffix: ".download.zip",
		SourceURL:      func() string { return ImagesSourceURL },
		CacheDir:       hypervCacheDir,
		Unpack:         unpackImage,
		InUse:          isImageInUse,
	})
}

func hypervCacheDir() (string, error) {
	return workspace.CacheSubDir("driver-hyperv")
}

// unpackImage turns a file that is given to the image store into the .vhdx
// file to check and cache. A .vhdx file is used as it is, and is never
// removed. A .zip file must have exactly one entry, which is the .vhdx file.
// It is unpacked into workdir, which the image store removes.
func unpackImage(path string, workdir string) (string, func(), error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".vhdx":
		return path, nil, nil
	case ".zip":
		unzipped, err := unzipImage(path, workdir)
		return unzipped, nil, err
	default:
		return "", nil, errors.New("only .vhdx or .zip files allowed")
	}
}

// unzipImage unpacks the only entry of a zip file into workdir, and returns
// the path of the file it made. The name of the entry is not used.
func unzipImage(zipfilepath string, workdir string) (string, error) {
	kuttilog.Println(kuttilog.Info, "Decompressing downloaded file...")

	unzippedfilepath := filepath.Join(workdir, "image.vhdx")
	kuttilog.Printf(kuttilog.Debug, "Decompressing '%v' into '%v'...", zipfilepath, unzippedfilepath)

	unzipper, err := zip.OpenReader(zipfilepath)
	if err != nil {
		return "", err
	}
	defer unzipper.Close()

	if len(unzipper.File) != 1 {
		return "", errors.New("invalid compressed image")
	}

	srcFile, err := unzipper.File[0].Open()
	if err != nil {
		return "", err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(unzippedfilepath)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(dstFile, srcFile)
	if closeerr := dstFile.Close(); err == nil {
		err = closeerr
	}
	if err != nil {
		return "", err
	}

	kuttilog.Println(kuttilog.Info, "Finished decompressing downloaded file.")
	return unzippedfilepath, nil
}

// isImageInUse reports whether the cached master image for k8sversion is
// currently set as the parent/backing file of any differencing disk in the VM
// disks directory. It is used to prevent removing or overwriting a master
// image out from under machines that depend on it.
func isImageInUse(k8sversion string) (bool, error) {
	imagePath, err := imagestore.ImagePath(k8sversion)
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
