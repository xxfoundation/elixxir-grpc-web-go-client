package transport

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"nhooyr.io/websocket"
	"strconv"
	"strings"
	"sync"
)

// webSocketUnaryTransport was written as an intermediary step in allowing
// server streams to run over websockets.  It is not currently in use, but
// remains in the codebase as an example/model
type webSocketUnaryTransport struct {
	host     string
	endpoint string

	conn *websocket.Conn

	once    sync.Once
	resOnce sync.Once

	closed bool

	writeMu sync.Mutex

	reqHeader, header, trailer http.Header
}

func (t *webSocketUnaryTransport) Header() http.Header {
	return t.reqHeader
}

func (t *webSocketUnaryTransport) GetRespHeader() (http.Header, error) {
	return t.header, nil
}

func (t *webSocketUnaryTransport) Trailer() http.Header {
	return t.trailer
}

func (t *webSocketUnaryTransport) SetRequestHeader(h http.Header) {
	t.reqHeader = h
}

func (t *webSocketUnaryTransport) GetRemoteCertificate() (*x509.Certificate, error) {
	return nil, nil
}

func (t *webSocketUnaryTransport) Send(ctx context.Context, _, _ string, body io.Reader) (http.Header, []byte, error) {
	if t.closed {
		return nil, nil, io.EOF
	}

	var err error
	t.once.Do(func() {
		h := t.reqHeader
		if h == nil {
			h = make(http.Header)
		}
		h.Set("content-type", "application/grpc-web+proto")
		h.Set("x-grpc-web", "1")
		var b bytes.Buffer
		h.Write(&b)

		t.writeMessage(int(websocket.MessageBinary), b.Bytes())
	})
	if err != nil {
		return nil, nil, err
	}

	var b bytes.Buffer
	b.Write([]byte{0x00})
	_, err = io.Copy(&b, body)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to read request body")
	}

	t.writeMessage(int(websocket.MessageBinary), b.Bytes())

	t.CloseSend()

	rc, err := t.Receive(ctx)
	if err != nil {
		return nil, nil, err
	}

	h, err := t.GetRespHeader()
	if err != nil {
		return nil, nil, err
	}

	msgBytes, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, nil, err
	}

	//msgH, err := parser.ParseResponseHeader(rc)
	//if err != nil {
	//	return nil, nil, err
	//}
	//msg, err := parser.ParseLengthPrefixedMessage(rc, msgH.ContentLength)
	//if err != nil {
	//	return nil, nil, err
	//}

	return h, msgBytes, nil
}

func (t *webSocketUnaryTransport) Receive(context.Context) (_ io.ReadCloser, err error) {
	if t.closed {
		return nil, io.EOF
	}

	defer func() {
		if err == nil {
			return
		}

		if berr, ok := errors.Cause(err).(*net.OpError); ok && !berr.Temporary() {
			err = io.EOF
		}
	}()

	// skip response header
	t.resOnce.Do(func() {
		ctx := context.Background()
		_, _, err = t.conn.Read(ctx)
		if err != nil {
			err = errors.Wrap(err, "failed to read response header")
			return
		}

		_, msg, err := t.conn.Reader(ctx)
		if err != nil {
			err = errors.Wrap(err, "failed to read response header")
			return
		}

		h := make(http.Header)
		s := bufio.NewScanner(msg)
		for s.Scan() {
			t := s.Text()
			i := strings.Index(t, ": ")
			if i == -1 {
				continue
			}
			k := strings.ToLower(t[:i])
			h.Add(k, t[i+2:])
		}
		t.header = h
	})

	var buf bytes.Buffer
	var b []byte

	numChunks := 1
	if t.header != nil {
		numChunkStr := t.header.Get("Totalchunks")
		numChunks, err = strconv.Atoi(numChunkStr)
		if err != nil {
			numChunks = 1
		}
	}

	fullMsg := bytes.NewBuffer(nil)

	for i := 0; i < numChunks; i++ {
		_, b, err = t.conn.Read(context.Background())
		if err != nil {
			if cerr, ok := err.(*websocket.CloseError); ok {
				if cerr.Code == websocket.StatusNormalClosure {
					return nil, io.EOF
				}
				if cerr.Code == websocket.StatusAbnormalClosure {
					return nil, io.ErrUnexpectedEOF
				}
			}
			err = errors.Wrap(err, "failed to read response body")
			return
		}
		buf.Write(b)

		var r io.Reader
		_, r, err = t.conn.Reader(context.Background())
		if err != nil {
			return
		}

		res := ioutil.NopCloser(io.MultiReader(&buf, r))

		by, err := ioutil.ReadAll(res)
		if err != nil {
			panic(err)
		}

		res = ioutil.NopCloser(bytes.NewReader(by))

		fullMsg.Write(by)
	}
	return ioutil.NopCloser(fullMsg), nil

}

func (t *webSocketUnaryTransport) CloseSend() error {
	// 0x01 means the finish send frame.
	// ref. transports/websocket/websocket.ts
	t.writeMessage(int(websocket.MessageBinary), []byte{0x01})
	return nil
}

func (t *webSocketUnaryTransport) Close() error {
	// Send the close message.
	if t.closed {
		return nil
	}

	err := t.conn.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		return err
	}
	t.closed = true
	// Close the WebSocket connection.
	return nil
}

func (t *webSocketUnaryTransport) writeMessage(msg int, b []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.conn.Write(context.Background(), websocket.MessageType(msg), b)
}

var NewWSUT = func(host, endpoint string, opts *ConnectOptions) (UnaryTransport, error) {
	var conn *websocket.Conn
	dialer := initWsDialer(opts)
	scheme := "ws"
	if opts.WithTLS {
		scheme = "wss"
	}
	u := url.URL{Scheme: scheme, Host: host, Path: endpoint}
	conn, _, err := websocket.Dial(context.Background(), u.String(), dialer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to dial to '%s'", u.String())
	}

	return &webSocketUnaryTransport{
		host:     host,
		endpoint: endpoint,
		conn:     conn,
	}, nil
}
