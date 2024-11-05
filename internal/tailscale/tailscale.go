package tailscale

import (
	"fmt"
	"net"
	"path/filepath"

	"github.com/almeidapaulopt/tsdproxy/internal/containers"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	tsclient "tailscale.com/client/tailscale"
	"tailscale.com/tsnet"
)

type TsNetServer struct {
	TsServer    *tsnet.Server
	LocalClient *tsclient.LocalClient
}

func NewTsNetServer(hostname string, config *core.Config, logger *core.Logger, ct *containers.Container) (*TsNetServer, error) {
	logger.Debug().
		Str("hostname", hostname).
		Bool("ephemeral", ct.Ephemeral).
		Bool("webclient", ct.WebClient).
		Bool("runWebClient", ct.WebClient).
		Msg("Setting up tailscale server")

	tserver := &tsnet.Server{
		Hostname:     hostname,
		AuthKey:      config.AuthKey,
		Dir:          filepath.Join(config.DataDir, hostname),
		Ephemeral:    ct.Ephemeral,
		RunWebClient: ct.WebClient,
		UserLogf:     logger.Info().Msgf,
		Logf:         logger.Trace().Msgf,
		ControlURL:   config.ControlURL,
	}

	if ct.TsnetVerbose {
		tserver.Logf = logger.Info().Msgf
	}

	lc, err := tserver.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("error starting tailscale server: %w", err)
	}

	return &TsNetServer{
		tserver,
		lc,
	}, nil
}

func (tn *TsNetServer) Close() error {
	return tn.TsServer.Close()
}

func (tn *TsNetServer) GetListen(ct *containers.Container) (net.Listener, error) {
	if ct.Funnel {
		return tn.TsServer.ListenFunnel("tcp", ":443")
	}

	return tn.TsServer.ListenTLS("tcp", ":443")
}
