package driverhyperv

import (
	"fmt"

	"github.com/kuttiproject/drivercore"
)

// UpdateImageList fetches the latest list of VM images from the driver source URL.
func (vd *Driver) UpdateImageList() error {
	return fetchimagelist()
}

// ValidK8sVersion returns true if the specified Kubernetes version is currently
// supported.
func (vd *Driver) ValidK8sVersion(k8sversion string) bool {
	err := imageconfigmanager.Load()
	if err != nil {
		return false
	}

	_, ok := imagedata.images[k8sversion]
	return ok
}

// K8sVersions returns all Kubernetes versions currently supported.
func (vd *Driver) K8sVersions() []string {
	err := imageconfigmanager.Load()
	if err != nil {
		return []string{}
	}

	result := make([]string, len(imagedata.images))
	index := 0
	for _, value := range imagedata.images {
		result[index] = value.imageK8sVersion
		index++
	}

	return result
}

// ListImages lists the currently available Images.
func (vd *Driver) ListImages() ([]drivercore.Image, error) {
	err := imageconfigmanager.Load()
	if err != nil {
		return []drivercore.Image{}, err
	}

	result := make([]drivercore.Image, len(imagedata.images))
	index := 0
	for _, value := range imagedata.images {
		result[index] = value
		index++
	}

	return result, nil
}

// GetImage returns an image corresponding to a Kubernetes version, or an error.
func (vd *Driver) GetImage(k8sversion string) (drivercore.Image, error) {
	err := imageconfigmanager.Load()
	if err != nil {
		return nil, err
	}

	img, ok := imagedata.images[k8sversion]
	if !ok {
		return img, fmt.Errorf("no image present for K8s version %s", k8sversion)
	}

	return img, nil
}
