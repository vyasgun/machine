package plugin

import (
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"path/filepath"
	"time"

	"github.com/crc-org/machine/libmachine/drivers"
	"github.com/crc-org/machine/libmachine/drivers/plugin/localbinary"
	rpcdriver "github.com/crc-org/machine/libmachine/drivers/rpc"
	"github.com/crc-org/machine/libmachine/version"
	log "github.com/sirupsen/logrus"
)

var (
	heartbeatTimeout = 10 * time.Second
)

func RegisterDriver(d drivers.Driver) {
	if os.Getenv(localbinary.PluginEnvKey) != localbinary.PluginEnvVal {
		fmt.Fprintf(os.Stderr, `This is a hypervisor plugin binary for CodeReady Containers.
Please use this plugin through the main 'crc' binary.
(Driver version: %s, API version: %d)
`, d.DriverVersion(),
			version.APIVersion)
		os.Exit(1)
	}

	log.SetLevel(log.DebugLevel)
	os.Setenv("MACHINE_DEBUG", "1")

	rpcd := rpcdriver.NewRPCServerDriver(d)
	if err := rpc.RegisterName(rpcdriver.RPCServiceNameV0, rpcd); err != nil {
		log.Error(err)
	}
	if err := rpc.RegisterName(rpcdriver.RPCServiceNameV1, rpcd); err != nil {
		log.Error(err)
	}
	rpc.HandleHTTP()

	socketDir := os.Getenv("CRC_SOCKET_DIR")
	if socketDir == "" {
		socketDir = filepath.Join(os.TempDir(), "crc-machine")
	}

	if err := os.MkdirAll(socketDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating socket directory: %s\n", err)
		os.Exit(1)
	}
	socketDirInfo, err := os.Stat(socketDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking socket directory: %s\n", err)
		os.Exit(1)
	}
	if !socketDirInfo.IsDir() || socketDirInfo.Mode().Perm()&0077 != 0 {
		fmt.Fprintf(os.Stderr, "Socket directory must be owner-only: %s\n", socketDir)
		os.Exit(1)
	}

	socketPath := filepath.Join(socketDir, "plugin.sock")
	os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading RPC server: %s\n", err)
		os.Exit(1)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		fmt.Fprintf(os.Stderr, "Error setting socket permissions: %s\n", err)
		os.Exit(1)
	}

	fmt.Println(listener.Addr())

	go func() {
		_ = http.Serve(listener, nil)
	}()

	for {
		select {
		case <-rpcd.CloseCh:
			log.Debug("Closing plugin on server side")
			os.Exit(0) //nolint
		case <-rpcd.HeartbeatCh:
			continue
		case <-time.After(heartbeatTimeout):
			// TODO: Add heartbeat retry logic
			os.Exit(1)
		}
	}
}
