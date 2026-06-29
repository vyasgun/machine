package plugin

import (
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"
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

type loggingListener struct {
	net.Listener
}

func (l *loggingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	log.WithField("remote", conn.RemoteAddr().String()).Info("RPC connection accepted")
	return &loggingConn{Conn: conn, start: time.Now()}, nil
}

type loggingConn struct {
	net.Conn
	start time.Time
}

func (c *loggingConn) Close() error {
	log.WithFields(log.Fields{
		"remote":   c.RemoteAddr().String(),
		"duration": time.Since(c.start),
	}).Info("RPC connection closed")
	return c.Conn.Close()
}

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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading RPC server: %s\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println(listener.Addr())

	go func() {
		//#nosec G114 localhost-only RPC server
		_ = http.Serve(&loggingListener{Listener: listener}, nil)
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
