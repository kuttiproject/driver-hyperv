# driver-hyperv

kutti driver for Microsoft Hyper-V

[![Go Report Card](https://goreportcard.com/badge/github.com/kuttiproject/driver-hyperv)](https://goreportcard.com/report/github.com/kuttiproject/driver-hyperv)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/kuttiproject/driver-hyperv)](https://pkg.go.dev/github.com/kuttiproject/driver-hyperv)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/kuttiproject/driver-hyperv?include_prereleases)

## Images

This driver depends on Hyper-V VM images published via the [kuttiproject/driver-hyperv-images](https://github.com/kuttiproject/driver-hyperv-images) repository. The details of the driver-to-VM interface are documented there.

The releases of that repository are the default source for this driver. The list of available/deprecated images and the images themselves are published there. The releases of that repository follow the major and minor versions of this repository, but sometimes may lag by one version. The `ImagesVersion` constant specifies the version of the images repository that is used by a particular version of this driver.

## Windows-only

This driver only works on the Windows family of operating systems.
