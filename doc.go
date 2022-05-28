// Package driverhyperv implements a kutti driver for Microsoft Hyper-V.
// It uses the Hyper-V PowerShell module to talk to Hyper-V. It invokes
// Cmdlets from the module via an interface script.
//
// For cluster networking, it uses the Hyper-V default switch. 
//
// For nodes, it creates virtual machines with pre-set settings, and
// attaches copies of VHDX disks, maintained by the companion 
// driver-hyperv-images project.
// For images, it uses the aforesaid VHDX files, downloading the list
// from the URL pointed to by the ImagesSourceURL variable.
//
// The details of individual operations can be found in the online
// documentation. Details about the interface between the driver and
// a running VM can be found at the driver-hyperv-images project:
// https://github.com/kuttiproject/driver-hyperv-images
package driverhyperv
