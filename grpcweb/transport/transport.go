package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"github.com/pkg/errors"
	jww "github.com/spf13/jwalterweatherman"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"nhooyr.io/websocket"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UnaryTransport is the public interface for the transport package
type UnaryTransport interface {
	Header() http.Header
	Send(ctx context.Context, endpoint, contentType string, body io.Reader) (http.Header, []byte, error)
	Close() error
}

type httpTransport struct {
	host       string
	client     *http.Client
	clientLock *sync.RWMutex
	opts       *ConnectOptions

	header http.Header
}

// IsAlive is not easily defined for grpcweb, return true if t.client != nil
func (t *httpTransport) IsAlive() bool {
	// No good way to check http connection status
	// return true if client is not nil
	return t.client != nil
}

// Header returns the http.Header object for httpTransport
func (t *httpTransport) Header() http.Header {
	return t.header
}

// Send accepts an endpoint, content-type header & body and sends them to
// a grpc-web server using http requests.
func (t *httpTransport) Send(ctx context.Context, endpoint, contentType string, body io.Reader) (http.Header, []byte, error) {
	var scheme string
	if t.opts.WithTLS {
		scheme = "https"
	} else {
		scheme = "http"
	}
	u := url.URL{Scheme: scheme, Host: t.host, Path: endpoint}
	reqUrl := u.String()
	req, err := http.NewRequest(http.MethodPost, reqUrl, body)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to build the API request")
	}

	req.Header = t.Header()
	req.Header.Add("content-type", contentType)
	req.Header.Add("x-grpc-web", "1")

	t.clientLock.RLock()
	defer t.clientLock.RUnlock()
	res, err := t.client.Do(req)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to send the API")
	}

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to read response body")
	}
	res.Close = true
	err = res.Body.Close()
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to close response body")
	}

	if grpcStatus := res.Header.Get("grpc-status"); grpcStatus != "" {
		grpcStatusInt, err := strconv.Atoi(res.Header.Get("grpc-status"))
		if err != nil {
			jww.WARN.Printf("Invalid GRPC status header: %+v", err)
		} else if grpcStatusInt > 0 {
			jww.WARN.Printf("received GRPC status code %d: %s",
				grpcStatusInt, res.Header.Get("grpc-message"))
		}
	}

	return res.Header, respBody, nil
}

// Close the httpTransport object
// Note that this just closes idle connections, to properly close this
// connection delete the object.
func (t *httpTransport) Close() error {
	t.clientLock.Lock()
	defer t.clientLock.Unlock()
	t.client.CloseIdleConnections()
	return nil
}

// NewUnary returns an httpTransport object wrapped as a UnaryTransport object
var NewUnary = func(host string, opts *ConnectOptions) UnaryTransport {
	cl := http.DefaultClient
	transport := &http.Transport{}
	if opts.WithTLS {
		certPool := x509.NewCertPool()
		decoded, _ := pem.Decode(opts.TLSCertificate)
		if decoded == nil {
			panic("failed to decode cert")
		}
		cert, err := x509.ParseCertificate(decoded.Bytes)
		if err != nil {
			panic(err)
		}
		certPool.AddCert(cert)
		transport.TLSClientConfig = &tls.Config{RootCAs: certPool, ServerName: cert.DNSNames[0]}
		transport.TLSClientConfig.InsecureSkipVerify = opts.TlsInsecureSkipVerify
	}
	if opts.Timeout != 0 {
		cl.Timeout = time.Second
	} else {
		cl.Timeout = opts.Timeout
	}

	if opts.IdleConnTimeout != 0 {
		transport.IdleConnTimeout = opts.IdleConnTimeout
	}

	if opts.TlsHandshakeTimeout != 0 {
		transport.TLSHandshakeTimeout = opts.TlsHandshakeTimeout
	}

	if opts.ExpectContinueTimeout != 0 {
		transport.ExpectContinueTimeout = opts.ExpectContinueTimeout
	}

	cl.Transport = transport
	return &httpTransport{
		host:       host,
		client:     cl,
		opts:       opts,
		header:     make(http.Header),
		clientLock: &sync.RWMutex{},
	}
}

type ClientStreamTransport interface {
	Header() (http.Header, error)
	Trailer() http.Header

	// SetRequestHeader sets headers to send gRPC-Web server.
	// It should be called before calling Send.
	SetRequestHeader(h http.Header)
	Send(ctx context.Context, body io.Reader) error
	Receive(ctx context.Context) (io.ReadCloser, error)

	// CloseSend sends a close signal to the server.
	CloseSend() error

	// Close closes the connection.
	Close() error
}

// webSocketTransport is a stream transport implementation.
//
// Currently, gRPC-Web specification does not support client streaming. (https://github.com/improbable-eng/grpc-web#client-side-streaming)
// webSocketTransport supports improbable-eng/grpc-web's own implementation.
//
// spec: https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-WEB.md
type webSocketTransport struct {
	host     string
	endpoint string

	conn *websocket.Conn

	once    sync.Once
	resOnce sync.Once

	closed bool

	writeMu sync.Mutex

	reqHeader, header, trailer http.Header
}

func (t *webSocketTransport) Header() (http.Header, error) {
	return t.header, nil
}

func (t *webSocketTransport) Trailer() http.Header {
	return t.trailer
}

func (t *webSocketTransport) SetRequestHeader(h http.Header) {
	t.reqHeader = h
}

func (t *webSocketTransport) Send(ctx context.Context, body io.Reader) error {
	if t.closed {
		return io.EOF
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
		return err
	}

	var b bytes.Buffer
	b.Write([]byte{0x00})
	_, err = io.Copy(&b, body)
	if err != nil {
		return errors.Wrap(err, "failed to read request body")
	}

	return t.writeMessage(int(websocket.MessageBinary), b.Bytes())
}

func (t *webSocketTransport) Receive(context.Context) (_ io.ReadCloser, err error) {
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
		_, _, err = t.conn.Reader(ctx)
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

	return res, nil
}

func (t *webSocketTransport) CloseSend() error {
	// 0x01 means the finish send frame.
	// ref. transports/websocket/websocket.ts
	t.writeMessage(int(websocket.MessageBinary), []byte{0x01})
	return nil
}

func (t *webSocketTransport) Close() error {
	// Send the close message.

	err := t.conn.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		return err
	}
	t.closed = true
	// Close the WebSocket connection.
	return nil
}

func (t *webSocketTransport) writeMessage(msg int, b []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.conn.Write(context.Background(), websocket.MessageType(msg), b)
}

var NewClientStream = func(host, endpoint string, opts *ConnectOptions) (ClientStreamTransport, error) {
	// TODO: WebSocket over TLS support.
	h := http.Header{}
	h.Set("Sec-WebSocket-Protocol", "grpc-websockets")
	var conn *websocket.Conn
	dialer := &websocket.DialOptions{}
	dialer.HTTPClient = http.DefaultClient
	scheme := "ws"
	if opts.WithTLS {
		scheme = "wss"
		certPool := x509.NewCertPool()
		decoded, _ := pem.Decode(opts.TLSCertificate)
		if decoded == nil {
			panic("failed to decode cert")
		}
		cert, err := x509.ParseCertificate(decoded.Bytes)
		if err != nil {
			panic(err)
		}
		certPool.AddCert(cert)
		tlsConf := &tls.Config{RootCAs: certPool, ServerName: cert.DNSNames[0]}
		tlsConf.InsecureSkipVerify = opts.TlsInsecureSkipVerify

		dialer.HTTPClient.Transport = &http.Transport{
			TLSClientConfig: tlsConf,
		}

	}
	u := url.URL{Scheme: scheme, Host: host, Path: endpoint}
	conn, _, err := websocket.Dial(context.Background(), u.String(), dialer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to dial to '%s'", u.String())
	}

	return &webSocketTransport{
		host:     host,
		endpoint: endpoint,
		conn:     conn,
	}, nil
}
