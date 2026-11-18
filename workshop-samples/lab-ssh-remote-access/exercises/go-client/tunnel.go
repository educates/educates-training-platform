// This is a reference implementation for ssh proxy command which can tunnel
// SSH into a workshop session. The argument should be the "wss://*/tunnel/" URL
// for accessing the tunnelling proxy, or the "ws://*/tunnel/" URL when the
// session is served over plain HTTP.

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

type session struct {
	ws      *websocket.Conn
	errChan chan error
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "tunnel URL",
		Short: "SSH proxy for tunnelling over websockets.",
		Run:   root,
	}

	rootCmd.Execute()
}

func root(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help()
		os.Exit(1)
	}

	dest, err := url.Parse(args[0])

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	originURL := *dest

	origin := originURL.String()

	headers := make(http.Header)
	headers.Add("Origin", origin)

	dialer := websocket.Dialer{}

	ws, _, err := dialer.Dial(origin, headers)

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sess := &session{
		ws:      ws,
		errChan: make(chan error),
	}

	go sess.readInput()
	go sess.readRemote()

	os.Stderr.WriteString(fmt.Sprintf("%s\n", <-sess.errChan))
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
				break
			}
		default:
			s.errChan <- fmt.Errorf("unexpected websocket frame type: %d", msgType)
			return
		}
	}
}
