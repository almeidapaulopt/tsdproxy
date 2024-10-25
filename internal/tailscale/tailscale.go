package tailscale

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"tailscale.com/tsnet"
)

type TsNetServer struct {
	TsServer *tsnet.Server
}

func NewTsNetServer(hostname string, config *core.Config, logger *core.Logger) *TsNetServer {
	return &TsNetServer{
		&tsnet.Server{
			Hostname:  hostname,
			AuthKey:   config.AuthKey,
			Dir:       filepath.Join(config.DataDir, hostname),
			Ephemeral: true,
			Logf: func(format string, args ...any) {
				logger.Trace().Msgf(format, args...)
			},
			UserLogf: func(format string, args ...any) {
				logger.Trace().Msgf(format, args...)
			},
		},
	}
}

func (tn *TsNetServer) Close() error {
	return tn.TsServer.Close()
}

func (tn *TsNetServer) Start(ctx context.Context) error {
	if err := tn.TsServer.Start(); err != nil {
		return fmt.Errorf("error starting server: %w", err)
	}

	// Wait for tailscale to come up...
	if _, err := tn.TsServer.Up(ctx); err != nil {
		return fmt.Errorf("error to come up server: %w", err)
	}

	return tn.TsServer.Start()
}
