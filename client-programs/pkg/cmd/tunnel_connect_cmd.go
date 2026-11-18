package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var tunnelConnectExample = `
  # Connect an SSH proxy through the websocket tunnel of the workshop session
  # served at lab-w01-s001.educates.example.com over HTTPS:
  educates tunnel connect --url wss://lab-w01-s001.educates.example.com/tunnel/

  # The same, for a session served over plain HTTP:
  educates tunnel connect --url ws://lab-w01-s001.10-10-10-2.nip.io/tunnel/
`

type TunnelConnectOptions struct {
	Url string
}

type session struct {
	ws      *websocket.Conn
	errChan chan error
}

func (o *TunnelConnectOptions) Run(cmd *cobra.Command) error {
	dest, err := url.Parse(o.Url)

	if err != nil {
		return errors.Wrap(err, "unable to parse websocket URL")
	}

	originURL := *dest

	origin := originURL.String()

	headers := make(http.Header)
	headers.Add("Origin", origin)

	dialer := websocket.Dialer{}

	ws, _, err := dialer.Dial(origin, headers)

	if err != nil {
		return errors.Wrap(err, "unable to connect to websocket URL")
	}

	sess := &session{
		ws:      ws,
		errChan: make(chan error),
	}

	go sess.readInput()
	go sess.readRemote()

	os.Stderr.WriteString(fmt.Sprintf("%s\n", <-sess.errChan))

	return nil
}

func (p *ProjectInfo) NewTunnelConnectCmd() *cobra.Command {
	var o TunnelConnectOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "connect",
		Short:   "SSH proxy for tunnelling over websockets",
		Example: tunnelConnectExample,
		RunE:    func(cmd *cobra.Command, _ []string) error { return o.Run(cmd) },
	}

	c.Flags().StringVar(
		&o.Url,
		"url",
		"",
		"websocket URL of the workshop session's tunnel: wss://<session host>/tunnel/, or ws:// when the session is served over plain HTTP",
	)

	c.MarkFlagRequired("url")

	return c
}

func (s *session) readInput() {
	in := os.Stdin

	const BUF_SIZE = 16384
	bufOut := make([]byte, BUF_SIZE)

	for {
		var n int
		var err error

		if n, err = in.Read(bufOut); err != nil || n == 0 {
			break
		}

		if err = s.ws.WriteMessage(websocket.BinaryMessage, bufOut[0:n]); err != nil {
			break
		}
	}
}

func (s *session) readRemote() {
	out := os.Stdout

	for {
		msgType, buf, err := s.ws.ReadMessage()

		if err != nil {
			s.errChan <- err
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			if _, err = out.Write(buf); err != nil {
				return
			}
		default:
			s.errChan <- fmt.Errorf("unexpected websocket frame type: %d", msgType)
			return
		}
	}
}
