package tunnel

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

type session struct {
	ws      *websocket.Conn
	errChan chan error
}

type Tunnel struct {
	Url string
}

func NewTunnel(url string) *Tunnel {
	return &Tunnel{
		Url: url,
	}
}

func (t *Tunnel) Start() error {
		dest, err := url.Parse(t.Url)

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
