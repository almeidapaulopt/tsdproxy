package tailscale

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"

	"github.com/almeidapaulopt/tsdproxy/internal/containers"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	tsclient "tailscale.com/client/tailscale"
	"tailscale.com/tsnet"
)

type TsNetServer struct {
	TsServer     *tsnet.Server
	LocalClient  *tsclient.LocalClient
	HTTPListener net.Listener
}

func NewTsNetServer(hostname string, config *core.Config, logger *core.Logger, ct *containers.Container) (*TsNetServer, error) {
	logger.Debug().
		Str("hostname", hostname).
		Bool("ephemeral", ct.Labels.Ephemeral).
		Bool("webclient", ct.Labels.WebClient).
		Bool("runWebClient", ct.Labels.WebClient).
		Msg("Setting up tailscale server")

	tserver := &tsnet.Server{
		Hostname:     hostname,
		AuthKey:      config.AuthKey,
		Dir:          filepath.Join(config.DataDir, hostname),
		Ephemeral:    ct.Labels.Ephemeral,
		RunWebClient: ct.Labels.WebClient,
		UserLogf:     logger.Info().Msgf,
		Logf:         logger.Trace().Msgf,
		ControlURL:   config.ControlURL,
	}

	if ct.Labels.TsnetVerbose {
		tserver.Logf = logger.Info().Msgf
	}

	lc, err := tserver.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("error starting tailscale server: %w", err)
	}

	return &TsNetServer{
		TsServer:     tserver,
		LocalClient:  lc,
		HTTPListener: nil,
	}, nil
}

func (tn *TsNetServer) StartRedirectServer() error {
	var err error
	tn.HTTPListener, err = tn.TsServer.Listen("tcp", ":80")
	if err != nil {
		return fmt.Errorf("error creating HTTP listener: %w", err)
	}

	go func() {
		httpServer := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := "https://" + r.Host + r.URL.RequestURI()
				http.Redirect(w, r, target, http.StatusMovedPermanently)
			}),
		}
		err := httpServer.Serve(tn.HTTPListener)
		if err != nil && err != http.ErrServerClosed {
			// Log the error, but don't stop the main server
			fmt.Printf("HTTP redirect server error: %v\n", err)
		}
	}()

	return nil
}

func (tn *TsNetServer) Close() error {
	if tn.HTTPListener != nil {
		tn.HTTPListener.Close()
	}
	return tn.TsServer.Close()
}

func (tn *TsNetServer) GetListen(ct *containers.Container) (net.Listener, error) {
	if ct.Labels.Funnel {
		return tn.TsServer.ListenFunnel("tcp", ":443")
	}

	return tn.TsServer.ListenTLS("tcp", ":443")
}
