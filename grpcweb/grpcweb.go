package grpcweb

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"

	"git.xx.network/elixxir/grpc-web-go-client/grpcweb/parser"
	"git.xx.network/elixxir/grpc-web-go-client/grpcweb/transport"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
)

type ClientConn struct {
	host        string
	dialOptions *DialOptions
	transport   transport.UnaryTransport
}

func DialContext(host string, opts ...DialOption) (*ClientConn, error) {
	opt := defaultDialOptions
	for _, o := range opts {
		o(&opt)
	}
	tr := transport.NewUnary(host, &transport.ConnectOptions{
		WithTLS:               !opt.insecure,
		TLSCertificate:        opt.tlsCertificate,
		TlsInsecureSkipVerify: opt.tlsInsecureVerification,
	})
	return &ClientConn{
		host:        host,
		dialOptions: &opt,
		transport:   tr,
	}, nil
}

func (c *ClientConn) IsAlive() bool {
	return c.transport != nil
}

func (c *ClientConn) Invoke(ctx context.Context, method string, args, reply interface{}, opts ...CallOption) error {
	co := c.applyCallOptions(opts)
	codec := co.codec

	if c.transport == nil {
		tr := transport.NewUnary(c.host, &transport.ConnectOptions{
			WithTLS:               !c.dialOptions.insecure,
			TLSCertificate:        c.dialOptions.tlsCertificate,
			TlsInsecureSkipVerify: c.dialOptions.tlsInsecureVerification,
		})
		c.transport = tr
	}

	r, err := encodeRequestBody(codec, args)
	if err != nil {
		return errors.Wrap(err, "failed to build the request body")
	}

	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		for k, v := range md {
			for _, vv := range v {
				c.transport.Header().Add(k, vv)
			}
		}
	}

	contentType := "application/grpc-web+" + codec.Name()
	header, rawBody, err := c.transport.Send(ctx, method, contentType, r)
	if err != nil {
		return errors.Wrap(err, "failed to send the request")
	}

	if co.header != nil {
		*co.header = toMetadata(header)
	}
	bodyReader := bytes.NewReader(rawBody)
	resHeader, err := parser.ParseResponseHeader(bodyReader)
	if err != nil {
		return errors.Wrap(err, "failed to parse response header")
	}

	if resHeader.IsMessageHeader() {
		resBody, err := parser.ParseLengthPrefixedMessage(bodyReader, resHeader.ContentLength)
		if err != nil {
			return errors.Wrap(err, "failed to parse the response body")
		}
		if err := codec.Unmarshal(resBody, reply); err != nil {
			return errors.Wrapf(err, "failed to unmarshal response body by codec %s", codec.Name())
		}

		resHeader, err = parser.ParseResponseHeader(bodyReader)
		if err != nil {
			return errors.Wrap(err, "failed to parse response header")
		}
	}
	if !resHeader.IsTrailerHeader() {
		return errors.New("unexpected header")
	}

	status, trailer, err := parser.ParseStatusAndTrailer(bodyReader, resHeader.ContentLength)
	if err != nil {
		return errors.Wrap(err, "failed to parse status and trailer")
	}
	if co.trailer != nil {
		*co.trailer = trailer
	}

	return status.Err()
}

func (c *ClientConn) NewClientStream(desc *grpc.StreamDesc, method string, opts ...CallOption) (ClientStream, error) {
	if !desc.ClientStreams {
		return nil, errors.New("not a client stream RPC")
	}
	tr, err := transport.NewClientStream(c.host, method)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new transport stream")
	}
	return &clientStream{
		endpoint:    method,
		transport:   tr,
		callOptions: c.applyCallOptions(opts),
	}, nil
}

func (c *ClientConn) NewServerStream(desc *grpc.StreamDesc, method string, opts ...CallOption) (ServerStream, error) {
	if !desc.ServerStreams {
		return nil, errors.New("not a server stream RPC")
	}
	return &serverStream{
		endpoint:    method,
		transport:   transport.NewUnary(c.host, nil),
		callOptions: c.applyCallOptions(opts),
	}, nil
}

func (c *ClientConn) NewBidiStream(desc *grpc.StreamDesc, method string, opts ...CallOption) (BidiStream, error) {
	if !desc.ServerStreams || !desc.ClientStreams {
		return nil, errors.New("not a bidi stream RPC")
	}
	stream, err := c.NewClientStream(desc, method, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new client stream")
	}
	return &bidiStream{
		clientStream: stream.(*clientStream),
	}, nil
}

func (c *ClientConn) Close() error {
	if c.transport == nil {
		return nil
	}
	err := c.transport.Close()
	if err != nil {
		return err
	}
	c.transport = nil
	return nil
}

func (c *ClientConn) applyCallOptions(opts []CallOption) *callOptions {
	callOpts := append(c.dialOptions.defaultCallOptions, opts...)
	callOptions := defaultCallOptions
	for _, o := range callOpts {
		o(&callOptions)
	}
	return &callOptions
}

// copied from rpc_util.go#msgHeader
const headerLen = 5

func header(body []byte) []byte {
	h := make([]byte, 5)
	h[0] = byte(0)
	binary.BigEndian.PutUint32(h[1:], uint32(len(body)))
	return h
}

// header (compressed-flag(1) + message-length(4)) + body
// TODO: compressed message
func encodeRequestBody(codec encoding.Codec, in interface{}) (io.Reader, error) {
	body, err := codec.Marshal(in)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal the request body")
	}
	buf := bytes.NewBuffer(make([]byte, 0, headerLen+len(body)))
	buf.Write(header(body))
	buf.Write(body)
	return buf, nil
}

func toMetadata(h http.Header) metadata.MD {
	if len(h) == 0 {
		return nil
	}
	md := metadata.New(nil)
	for k, v := range h {
		md.Append(k, v...)
	}
	return md
}
