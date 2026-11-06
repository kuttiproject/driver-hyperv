package driverhyperv

import (
	"github.com/kuttiproject/drivercore"
)

// UpdateImageList fetches the latest list of VM images from the driver source URL.
func (vd *Driver) UpdateImageList() error {
	return imagestore.UpdateImageList()
}

// ValidK8sVersion returns true if the specified Kubernetes version is currently
// supported.
func (vd *Driver) ValidK8sVersion(k8sversion string) bool {
	return imagestore.ValidK8sVersion(k8sversion)
}

// K8sVersions returns all Kubernetes versions currently supported.
func (vd *Driver) K8sVersions() []string {
	return imagestore.K8sVersions()
}

// ListImages lists the currently available Images.
func (vd *Driver) ListImages() ([]drivercore.Image, error) {
	return imagestore.ListImages()
}

// GetImage returns an image corresponding to a Kubernetes version, or an error.
func (vd *Driver) GetImage(k8sversion string) (drivercore.Image, error) {
	return imagestore.GetImage(k8sversion)
}
