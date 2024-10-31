package tailscale

import (
	"fmt"
	"path/filepath"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	tsclient "tailscale.com/client/tailscale"
	"tailscale.com/tsnet"
)

type TsNetServer struct {
	TsServer    *tsnet.Server
	LocalClient *tsclient.LocalClient
}

func NewTsNetServer(hostname string, config *core.Config, logger *core.Logger) (*TsNetServer, error) {
	tserver := &tsnet.Server{
		Hostname:     hostname,
		AuthKey:      config.AuthKey,
		Dir:          filepath.Join(config.DataDir, hostname),
		Ephemeral:    true,
		RunWebClient: true,
		Logf: func(format string, args ...any) {
			logger.Trace().Msgf(format, args...)
		},
		UserLogf: func(format string, args ...any) {
			logger.Trace().Msgf(format, args...)
		},
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
