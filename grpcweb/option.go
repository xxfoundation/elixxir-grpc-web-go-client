package grpcweb

import (
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/metadata"
	"time"
)

var (
	defaultDialOptions = DialOptions{}
	defaultCallOptions = callOptions{
		codec: encoding.GetCodec(proto.Name),
	}
)

type DialOptions struct {
	defaultCallOptions      []CallOption
	insecure                bool
	transportCredentials    credentials.TransportCredentials
	tlsCertificate          []byte
	tlsInsecureVerification bool
	expectContinueTimeout   time.Duration
	idleConnTimeout         time.Duration
	tlsHandshakeTimeout     time.Duration
}

type DialOption func(*DialOptions)

func WithDefaultCallOptions(opts ...CallOption) DialOption {
	return func(opt *DialOptions) {
		opt.defaultCallOptions = opts
	}
}

func WithExpectContinueTimeout(duration time.Duration) DialOption {
	return func(opt *DialOptions) {
		opt.expectContinueTimeout = duration
	}
}

func WithTlsHandshakeTimeout(duration time.Duration) DialOption {
	return func(opt *DialOptions) {
		opt.tlsHandshakeTimeout = duration
	}
}

func WithIdleConnTimeout(duration time.Duration) DialOption {
	return func(opt *DialOptions) {
		opt.idleConnTimeout = duration
	}
}

func WithTlsCertificate(cert []byte) DialOption {
	return func(opt *DialOptions) {
		opt.tlsCertificate = cert
	}
}

func WithSecure() DialOption {
	return func(opt *DialOptions) {
		opt.insecure = false
		opt.tlsInsecureVerification = false
	}
}

func WithInsecureTlsVerification() DialOption {
	return func(opt *DialOptions) {
		opt.tlsInsecureVerification = true
	}
}

func WithInsecure() DialOption {
	return func(opt *DialOptions) {
		opt.insecure = true
	}
}

func WithTransportCredentials(creds credentials.TransportCredentials) DialOption {
	return func(opt *DialOptions) {
		opt.transportCredentials = creds
	}
}

type callOptions struct {
	codec           encoding.Codec
	header, trailer *metadata.MD
}

type CallOption func(*callOptions)

func CallContentSubtype(contentSubtype string) CallOption {
	return func(opt *callOptions) {
		opt.codec = encoding.GetCodec(contentSubtype)
	}
}

func Header(h *metadata.MD) CallOption {
	return func(opt *callOptions) {
		*h = metadata.New(nil)
		opt.header = h
	}
}

func Trailer(t *metadata.MD) CallOption {
	return func(opt *callOptions) {
		*t = metadata.New(nil)
		opt.trailer = t
	}
}
