package driverhyperv_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	driverhyperv "github.com/kuttiproject/driver-hyperv"
	"github.com/kuttiproject/drivercore/drivercoretest"
	"github.com/kuttiproject/kuttilog"
	"github.com/kuttiproject/workspace"
)

// The version and checksum of the driver-hyperv image
// to use for the test.
const (
	TESTK8SVERSION  = "1.27"
	TESTK8SCHECKSUM = "0c5487db0a68b60ad5eb8cf4897d24cd6d1731599f5d1c08e76be0d418b59f29"
)

func TestDriverHyperV(t *testing.T) {
	kuttilog.SetLogLevel(kuttilog.Debug)

	// Set up dummy web server for updating image list
	// and downloading image
	_, err := os.Stat(fmt.Sprintf("out/testserver/kutti-%v.vhdx.zip", TESTK8SVERSION))
	if err != nil {
		t.Fatalf(
			"Please download the version %v kutti hyper-v image, and place it in the path out/testserver/kutti-%v.vhdx.zip",
			TESTK8SVERSION,
			TESTK8SVERSION,
		)
	}

	serverMux := http.NewServeMux()
	server := http.Server{Addr: "localhost:8181", Handler: serverMux}
	defer server.Shutdown(context.Background())

	serverMux.HandleFunc(
		"/images.json",
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(
				w,
				`{"%v":{"ImageK8sVersion":"%v","ImageChecksum":"%v","ImageStatus":"NotDownloaded", "ImageSourceURL":"http://localhost:8181/kutti-%v.vhdx.zip"}}`,
				TESTK8SVERSION,
				TESTK8SVERSION,
				TESTK8SCHECKSUM,
				TESTK8SVERSION,
			)
		},
	)

	serverMux.HandleFunc(
		fmt.Sprintf("/kutti-%v.vhdx.zip", TESTK8SVERSION),
		func(rw http.ResponseWriter, r *http.Request) {
			http.ServeFile(
				rw,
				r,
				fmt.Sprintf("out/testserver/kutti-%v.vhdx.zip", TESTK8SVERSION),
			)
		},
	)

	go func() {
		t.Log("Server starting...")
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			t.Logf("ERROR:%v", err)
		}
		t.Log("Server stopped.")
	}()

	t.Log("Waiting 5 seconds for dummy server to start.")

	<-time.After(5 * time.Second)

	err = workspace.Set("out")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	driverhyperv.ImagesSourceURL = "http://localhost:8181/images.json"

	drivercoretest.TestDriver(t, "hyperv", TESTK8SVERSION)
}
