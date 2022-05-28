package driverhyperv

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kuttiproject/drivercore"
	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/workspace"
)

// hypervimagedata is a data-only representation of the Cluster type,
// used for serialization and output.
type hypervimagedata struct {
	ImageK8sVersion string
	ImageChecksum   string
	ImageSourceURL  string
	ImageStatus     drivercore.ImageStatus
	ImageDeprecated bool
}

// Image implements the drivercore.Image interface for VirtualBox.
type Image struct {
	imageK8sVersion string
	imageChecksum   string
	imageSourceURL  string
	imageStatus     drivercore.ImageStatus
	imageDeprecated bool
}

// K8sVersion returns the version of Kubernetes present in the image.
func (i *Image) K8sVersion() string {
	return i.imageK8sVersion
}

// Status returns the status of the image.
// Status can be Downloaded, meaning the image exists in the local cache and can
// be used to create Machines, or Notdownloaded, meaning it has to be downloaded
// using Fetch.
func (i *Image) Status() drivercore.ImageStatus {
	return i.imageStatus
}

// Deprecated returns true if the image's version of Kubenetes is deprecated.
// New Macines should not be created from such an image.
func (i *Image) Deprecated() bool {
	return i.imageDeprecated
}

// Fetch downloads the image from its source URL.
func (i *Image) Fetch() error {
	cachedir, err := hypervCacheDir()
	if err != nil {
		return err
	}

	// Images are zip files for this driver
	tempfilename := fmt.Sprintf("kutti-k8s-%s.download.zip", i.imageK8sVersion)
	tempfilepath := filepath.Join(cachedir, tempfilename)

	// Download file
	err = workspace.DownloadFile(i.imageSourceURL, tempfilepath)
	if err != nil {
		return err
	}
	defer workspace.RemoveFile(tempfilepath)

	kuttilog.Println(kuttilog.Debug, "Decompressing downloaded file...")
	// Unzip file
	unzippedfilename := fmt.Sprintf("kutti-k8s-%s.download", i.imageK8sVersion)
	unzippedfilepath := filepath.Join(cachedir, unzippedfilename)

	unzipper, err := zip.OpenReader(tempfilepath)
	if err != nil {
		return err
	}

	defer unzipper.Close()

	if len(unzipper.File) > 1 {
		return errors.New("invalid compressed image")
	}

	srcFile, err := unzipper.File[0].Open()
	if err != nil {
		return err
	}

	defer srcFile.Close()

	dstFile, err := os.Create(unzippedfilepath)
	if err != nil {
		return err
	}

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		os.Remove(unzippedfilepath)
	}

	dstFile.Close()
	defer workspace.RemoveFile(unzippedfilepath)
	kuttilog.Println(kuttilog.Debug, "Finished decompressing downloaded file.")

	// Add
	return i.FromFile(unzippedfilepath)
}

// FromFile verifies an image file on a local path and copies it to the cache.
func (i *Image) FromFile(filepath string) error {
	err := addfromfile(i.imageK8sVersion, filepath, i.imageChecksum)
	if err != nil {
		return err
	}

	i.imageStatus = drivercore.ImageStatusDownloaded
	return imageconfigmanager.Save()
}

// PurgeLocal removes the local cached copy of an image.
func (i *Image) PurgeLocal() error {
	if i.imageStatus == drivercore.ImageStatusDownloaded {
		err := removefile(i.K8sVersion())
		if err == nil {
			i.imageStatus = drivercore.ImageStatusNotDownloaded

			return imageconfigmanager.Save()
		}
		return err
	}

	return nil
}

// MarshalJSON returns the JSON encoding of the cluster.
func (i *Image) MarshalJSON() ([]byte, error) {
	savedata := hypervimagedata{
		ImageK8sVersion: i.imageK8sVersion,
		ImageChecksum:   i.imageChecksum,
		ImageSourceURL:  i.imageSourceURL,
		ImageStatus:     i.imageStatus,
		ImageDeprecated: i.imageDeprecated,
	}

	return json.Marshal(savedata)
}

// UnmarshalJSON  parses and restores a JSON-encoded
// cluster.
func (i *Image) UnmarshalJSON(b []byte) error {
	var loaddata hypervimagedata

	err := json.Unmarshal(b, &loaddata)
	if err != nil {
		return err
	}

	i.imageK8sVersion = loaddata.ImageK8sVersion
	i.imageChecksum = loaddata.ImageChecksum
	i.imageSourceURL = loaddata.ImageSourceURL
	i.imageStatus = loaddata.ImageStatus
	i.imageDeprecated = loaddata.ImageDeprecated

	return nil
}
