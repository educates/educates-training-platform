package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/tunnel"
	"github.com/spf13/cobra"
)

type TunnelConnectOptions struct {
	Url string
}

func (p *ProjectInfo) NewTunnelConnectCmd() *cobra.Command {
	var o TunnelConnectOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "connect",
		Short: "SSH proxy for tunnelling over websockets",
		RunE:  func(cmd *cobra.Command, _ []string) error { return tunnel.NewTunnel(o.Url).Start() },
	}

	c.Flags().StringVar(
		&o.Url,
		"url",
		"",
		"URL of websocket for connecting to workshop session",
	)

	c.MarkFlagRequired("url")

	return c
}
