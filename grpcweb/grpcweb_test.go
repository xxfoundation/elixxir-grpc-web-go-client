package grpcweb

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"git.xx.network/elixxir/grpc-web-go-client/grpcweb/transport"
	"github.com/google/go-cmp/cmp"
	"github.com/ktr0731/grpc-test/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type unaryTransport struct {
	t *testing.T

	expectedMD metadata.MD
	h          http.Header
	r          []byte
	err        error
}

func (t *unaryTransport) GetRemoteCertificate() (*x509.Certificate, error) {
	panic("UNIMPLEMENTED")
	return nil, nil
}

func (t *unaryTransport) Header() http.Header {
	return make(http.Header)
}

func (t *unaryTransport) Send(ctx context.Context, endpoint, contentType string, body io.Reader) (http.Header, []byte, error) {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.t.Fatalf("outgoing ctx should have metadata")
	}
	if diff := cmp.Diff(t.expectedMD, md); diff != "" {
		t.t.Fatalf("-want, +got\n%s", diff)
	}
	return t.h, t.r, t.err
}

func (t *unaryTransport) Close() error {
	return nil
}

func TestInvoke(t *testing.T) {
	header := http.Header{
		"hakase": []string{"shinonome"},
		"nano":   []string{"shinonome"},
	}

	cases := map[string]struct {
		transportHeader          http.Header
		transportContentFileName string
		expectedHeader           metadata.MD
		expectedTrailer          metadata.MD
		expectedContent          api.SimpleResponse
		expectedStatus           *status.Status
		wantErr                  bool
	}{
		"normal (only response)": {
			transportHeader:          header,
			transportContentFileName: "response.in",
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedContent: api.SimpleResponse{Message: "hello, ktr"},
			expectedStatus:  status.New(codes.OK, ""),
		},
		"normal (trailer and response)": {
			transportHeader:          header,
			transportContentFileName: "trailer_response.in",
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedTrailer: metadata.New(map[string]string{
				"trailer_key1": "trailer_val1",
				"trailer_key2": "trailer_val2",
			}),
			expectedContent: api.SimpleResponse{Message: "response"},
			expectedStatus:  status.New(codes.OK, ""),
		},
		"error (trailer and response)": {
			transportHeader:          header,
			transportContentFileName: "trailer_response_error.in",
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedTrailer: metadata.New(map[string]string{
				"trailer_key1": "trailer_val1",
				"trailer_key2": "trailer_val2",
			}),
			expectedStatus: status.New(codes.Internal, "internal error"),
			wantErr:        true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := os.Open(filepath.Join("testdata", c.transportContentFileName))
			if err != nil {
				t.Fatalf("Open should not return an error, but got '%s'", err)
			}

			rBytes, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}

			md := metadata.Pairs("yuko", "aioi")

			injectUnaryTransport(t, &unaryTransport{
				t:          t,
				expectedMD: md,
				h:          c.transportHeader,
				r:          rBytes,
			})

			var header, trailer metadata.MD
			client, err := DialContext(":50051")
			if err != nil {
				t.Fatalf("DialContext should not return an error, but got '%s'", err)
			}

			var res api.SimpleResponse
			opts := []CallOption{Header(&header), Trailer(&trailer)}
			req := api.SimpleRequest{Name: "nano"}
			ctx := metadata.NewOutgoingContext(context.Background(), md)
			err = client.Invoke(ctx, "/service/Method", &req, &res, opts...)
			if c.wantErr {
				if err == nil {
					t.Fatalf("should return an error, but got nil")
				}
			} else if err != nil {
				t.Fatalf("should not return an error, but got '%s'", err)
			}

			stat := status.Convert(err)

			if diff := cmp.Diff(c.expectedHeader, header); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if diff := cmp.Diff(c.expectedTrailer, trailer); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if diff := cmp.Diff(c.expectedContent.String(), res.String()); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if stat.Code() != c.expectedStatus.Code() {
				t.Errorf("expected status code: %s, but got %s", c.expectedStatus.Code(), stat.Code())
			}
			if stat.Message() != c.expectedStatus.Message() {
				t.Errorf("expected status message: %s, but got %s", c.expectedStatus.Message(), stat.Message())
			}
		})
	}
}

func injectUnaryTransport(t *testing.T, tr transport.UnaryTransport) {
	old := transport.NewUnary
	t.Cleanup(func() {
		transport.NewUnary = old
	})
	transport.NewUnary = func(string, *transport.ConnectOptions) transport.UnaryTransport {
		return tr
	}
}

func TestServerStream(t *testing.T) {
	header := http.Header{
		"hakase": []string{"shinonome"},
		"nano":   []string{"shinonome"},
	}

	cases := map[string]struct {
		transportHeader           http.Header
		transportContentFileNames []string
		expectedHeader            metadata.MD
		expectedTrailer           metadata.MD
		expectedContent           []api.SimpleResponse
		expectedStatus            *status.Status
		transportErr              error
	}{
		"normal (only response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"bidi_stream_response1.in", "bidi_stream_response2.in", "bidi_stream_response3.in", "bidi_stream_response4.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedContent: []api.SimpleResponse{
				{Message: "hello ktr, I greet 1 times."},
				{Message: "hello ktr, I greet 2 times."},
				{Message: "hello ktr, I greet 3 times."},
			},
			expectedStatus: status.New(codes.OK, ""),
		},
		"normal (trailer and response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"bidi_stream_response1.in", "bidi_stream_response2.in", "bidi_stream_response3.in", "bidi_stream_response1.in", "bidi_stream_response2.in", "bidi_stream_response3.in", "bidi_stream_trailer_response.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedTrailer: metadata.New(map[string]string{
				"trailer_key1": "trailer_val1",
				"trailer_key2": "trailer_val2",
			}),
			expectedContent: []api.SimpleResponse{
				{Message: "hello ktr, I greet 1 times."},
				{Message: "hello ktr, I greet 2 times."},
				{Message: "hello ktr, I greet 3 times."},
				{Message: "hello ktr, I greet 1 times."},
				{Message: "hello ktr, I greet 2 times."},
				{Message: "hello ktr, I greet 3 times."},
			},
			expectedStatus: status.New(codes.OK, ""),
		},
		//"error (trailer and response)": {
		//	transportHeader:           header,
		//	transportContentFileNames: []string{"bidi_stream_response1.in", "server_stream_trailer_response_error.in"},
		//	expectedHeader: metadata.New(map[string]string{
		//		"hakase": "shinonome",
		//		"nano":   "shinonome",
		//	}),
		//	expectedTrailer: metadata.New(map[string]string{
		//		"trailer_key1": "trailer_val1",
		//		"trailer_key2": "trailer_val2",
		//	}),
		//	expectedContent: []api.SimpleResponse{
		//		{Message: "hello nano, I greet 1 times."},
		//	},
		//	transportErr:   io.ErrUnexpectedEOF,
		//	expectedStatus: status.New(codes.Internal, "internal error"),
		//},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var rs []io.ReadCloser
			for _, fname := range c.transportContentFileNames {
				r, err := os.Open(filepath.Join("testdata", fname))
				if err != nil {
					t.Fatalf("Open should not return an error, but got '%s'", err)
				}
				rs = append(rs, r)
			}

			//rBytes, err := io.ReadAll(r)
			//if err != nil {
			//	t.Fatal(err)
			//}
			//fmt.Println(rBytes)

			md := metadata.Pairs("yuko", "aioi")

			//injectUnaryTransport(t, &unaryTransport{
			//	t:          t,
			//	expectedMD: md,
			//	h:          c.transportHeader,
			//	r:          rBytes,
			//})

			h := make(http.Header)
			h.Add("yuko", "aioi")
			injectClientStreamTransport(t, &clientStreamTransport{
				tt:             t,
				expectedHeader: h,
				h:              c.transportHeader,
				r:              rs,
				err:            c.transportErr,
			})

			client, err := DialContext(":50051")
			if err != nil {
				t.Fatalf("DialContext should not return an error, but got '%s'", err)
			}

			stm, err := client.NewServerStream(&grpc.StreamDesc{ServerStreams: true}, "/service/Method")
			if err != nil {
				t.Fatalf("should not return an error, but got '%s'", err)
			}

			ctx := metadata.NewOutgoingContext(context.Background(), md)
			if err := stm.Send(ctx, &api.SimpleRequest{Name: "nano"}); err != nil {
				t.Fatalf("Send should not return an error, but got '%s'", err)
			}

			var ress []api.SimpleResponse
			for {
				var res api.SimpleResponse
				err = stm.Receive(ctx, &res) // Don't create scoped error
				if errors.Is(err, io.EOF) {
					err = nil
					break
				}
				if err != nil {
					t.Logf("Receive returns an error: %s", err)
					break
				}
				ress = append(ress, res)
			}

			stat := status.Convert(err)

			header, err := stm.Header()
			if err != nil {
				t.Fatalf("Header should not return an error, but got '%s'", err)
			}
			if diff := cmp.Diff(c.expectedHeader, header); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if diff := cmp.Diff(c.expectedTrailer, stm.Trailer()); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			for i := 0; i < len(c.expectedContent); i++ {
				if diff := cmp.Diff(c.expectedContent[i].String(), ress[i].String()); diff != "" {
					t.Errorf("-want, +got\n%s", diff)
				}
			}

			if stat.Code() != c.expectedStatus.Code() {
				t.Errorf("expected status code: %s, but got %s", c.expectedStatus.Code(), stat.Code())
			}
			if stat.Message() != c.expectedStatus.Message() {
				t.Errorf("expected status message: %s, but got %s", c.expectedStatus.Message(), stat.Message())
			}
		})
	}
}

type clientStreamTransport struct {
	tt             *testing.T
	expectedHeader http.Header

	sentCloseSend bool

	h, t http.Header
	r    []io.ReadCloser
	err  error

	i int
}

func (s *clientStreamTransport) SetRequestHeader(h http.Header) {
	if diff := cmp.Diff(s.expectedHeader, h); diff != "" {
		s.tt.Fatalf("-want, +got\n%s", diff)
	}
}

func (s *clientStreamTransport) Header() (http.Header, error) {
	return s.h, nil
}

func (s *clientStreamTransport) Trailer() http.Header {
	return s.t
}

func (s *clientStreamTransport) Send(context.Context, io.Reader) error {
	return nil
}

func (s *clientStreamTransport) Receive(context.Context) (io.ReadCloser, error) {
	if s.sentCloseSend && s.err != nil {
		return nil, s.err
	}
	r := s.r[s.i]
	s.i++
	return r, nil
}

func (s *clientStreamTransport) CloseSend() error {
	s.sentCloseSend = true
	return nil
}

func (s *clientStreamTransport) Close() error {
	return nil
}

func TestClientStream(t *testing.T) {
	header := http.Header{
		"hakase": []string{"shinonome"},
		"nano":   []string{"shinonome"},
	}

	cases := map[string]struct {
		transportHeader           http.Header
		transportContentFileNames []string
		transportErr              error
		expectedHeader            metadata.MD
		expectedTrailer           metadata.MD
		expectedContent           api.SimpleResponse
		expectedStatus            *status.Status
	}{
		"normal (only response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"client_stream_response1.in", "client_stream_response2.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedContent: api.SimpleResponse{
				Message: "you sent requests 2 times (hakase, nano).",
			},
			expectedStatus: status.New(codes.OK, ""),
		},
		"normal (trailer and response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"client_stream_trailer_response1.in", "client_stream_trailer_response2.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedTrailer: metadata.New(map[string]string{
				"trailer_key1": "trailer_val1",
				"trailer_key2": "trailer_val2",
			}),
			expectedContent: api.SimpleResponse{
				Message: "you sent requests 2 times (hakase, nano).",
			},
			expectedStatus: status.New(codes.OK, ""),
		},
		"error (only response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"client_stream_response_error.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedStatus: status.New(codes.Internal, "internal error"),
		},
		"error (trailer only)": {
			transportHeader: http.Header{
				"hakase":       []string{"shinonome"},
				"nano":         []string{"shinonome"},
				"grpc-status":  []string{"13"},
				"grpc-message": []string{"internal error"},
			},
			transportErr:   io.ErrUnexpectedEOF,
			expectedHeader: nil,
			expectedTrailer: metadata.New(map[string]string{
				"hakase":       "shinonome",
				"nano":         "shinonome",
				"grpc-status":  "13",
				"grpc-message": "internal error",
			}),
			expectedStatus: status.New(codes.Internal, "internal error"),
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var rs []io.ReadCloser
			for _, fname := range c.transportContentFileNames {
				r, err := os.Open(filepath.Join("testdata", fname))
				if err != nil {
					t.Fatalf("Open should not return an error, but got '%s'", err)
				}
				rs = append(rs, r)
			}

			h := make(http.Header)
			h.Add("yuko", "aioi")
			injectClientStreamTransport(t, &clientStreamTransport{
				tt:             t,
				expectedHeader: h,
				h:              c.transportHeader,
				r:              rs,
				err:            c.transportErr,
			})

			client, err := DialContext(":50051", WithInsecure(), WithInsecureTlsVerification())
			if err != nil {
				t.Fatalf("DialContext should not return an error, but got '%s'", err)
			}

			stm, err := client.NewClientStream(&grpc.StreamDesc{ClientStreams: true}, "/service/Method")
			if err != nil {
				t.Fatalf("should not return an error, but got '%s'", err)
			}

			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("yuko", "aioi"))
			if err := stm.Send(ctx, &api.SimpleRequest{Name: "nano"}); err != nil {
				t.Fatalf("Send should not return an error, but got '%s'", err)
			}
			if err := stm.Send(ctx, &api.SimpleRequest{Name: "hakase"}); err != nil {
				t.Fatalf("Send should not return an error, but got '%s'", err)
			}

			var res api.SimpleResponse
			err = stm.CloseAndReceive(ctx, &res)

			stat := status.Convert(err)

			header, err := stm.Header()
			if err != nil {
				t.Fatalf("Header should not return an error, but got '%s'", err)
			}
			if diff := cmp.Diff(c.expectedHeader, header); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if diff := cmp.Diff(c.expectedTrailer, stm.Trailer()); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if diff := cmp.Diff(c.expectedContent.String(), res.String()); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if stat.Code() != c.expectedStatus.Code() {
				t.Errorf("expected status code: %s, but got %s", c.expectedStatus.Code(), stat.Code())
			}
			if stat.Message() != c.expectedStatus.Message() {
				t.Errorf("expected status message: %s, but got %s", c.expectedStatus.Message(), stat.Message())
			}
		})
	}
}

func TestBidiStream(t *testing.T) {
	header := http.Header{
		"hakase": []string{"shinonome"},
		"nano":   []string{"shinonome"},
	}

	cases := map[string]struct {
		transportHeader           http.Header
		transportContentFileNames []string
		transportErr              error
		expectedHeader            metadata.MD
		expectedTrailer           metadata.MD
		expectedContent           []api.SimpleResponse
		expectedStatus            *status.Status
	}{
		"normal (only response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"bidi_stream_response1.in", "bidi_stream_response2.in", "bidi_stream_response3.in", "bidi_stream_response4.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedContent: []api.SimpleResponse{
				{Message: "hello ktr, I greet 1 times."},
				{Message: "hello ktr, I greet 2 times."},
				{Message: "hello ktr, I greet 3 times."},
			},
			expectedStatus: status.New(codes.OK, ""),
		},
		"normal (trailer and response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"bidi_stream_response1.in", "bidi_stream_response2.in", "bidi_stream_response3.in", "bidi_stream_response1.in", "bidi_stream_response2.in", "bidi_stream_response3.in", "bidi_stream_trailer_response.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedTrailer: metadata.New(map[string]string{
				"trailer_key1": "trailer_val1",
				"trailer_key2": "trailer_val2",
			}),
			expectedContent: []api.SimpleResponse{
				{Message: "hello ktr, I greet 1 times."},
				{Message: "hello ktr, I greet 2 times."},
				{Message: "hello ktr, I greet 3 times."},
				{Message: "hello ktr, I greet 1 times."},
				{Message: "hello ktr, I greet 2 times."},
				{Message: "hello ktr, I greet 3 times."},
			},
			expectedStatus: status.New(codes.OK, ""),
		},
		"error (only response)": {
			transportHeader:           header,
			transportContentFileNames: []string{"bidi_stream_response_error.in"},
			expectedHeader: metadata.New(map[string]string{
				"hakase": "shinonome",
				"nano":   "shinonome",
			}),
			expectedStatus: status.New(codes.Internal, "internal error"),
		},
		"error (trailer only)": {
			transportHeader: http.Header{
				"hakase":       []string{"shinonome"},
				"nano":         []string{"shinonome"},
				"grpc-status":  []string{"13"},
				"grpc-message": []string{"internal error"},
			},
			transportErr:   io.ErrUnexpectedEOF,
			expectedHeader: nil,
			expectedTrailer: metadata.New(map[string]string{
				"hakase":       "shinonome",
				"nano":         "shinonome",
				"grpc-status":  "13",
				"grpc-message": "internal error",
			}),
			expectedStatus: status.New(codes.Internal, "internal error"),
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var rs []io.ReadCloser
			for _, fname := range c.transportContentFileNames {
				r, err := os.Open(filepath.Join("testdata", fname))
				if err != nil {
					t.Fatalf("Open should not return an error, but got '%s'", err)
				}
				rs = append(rs, r)
			}

			h := make(http.Header)
			h.Add("yuko", "aioi")
			injectClientStreamTransport(t, &clientStreamTransport{
				tt:             t,
				expectedHeader: h,
				h:              c.transportHeader,
				r:              rs,
				err:            c.transportErr,
			})

			client, err := DialContext(":50051", WithInsecure())
			if err != nil {
				t.Fatalf("DialContext should not return an error, but got '%s'", err)
			}

			stm, err := client.NewBidiStream(&grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, "/service/Method")
			if err != nil {
				t.Fatalf("should not return an error, but got '%s'", err)
			}

			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("yuko", "aioi"))
			if err := stm.Send(ctx, &api.SimpleRequest{Name: "nano"}); err != nil {
				t.Fatalf("Send should not return an error, but got '%s'", err)
			}
			if err := stm.Send(ctx, &api.SimpleRequest{Name: "hakase"}); err != nil {
				t.Fatalf("Send should not return an error, but got '%s'", err)
			}
			err = stm.CloseSend()

			var ress []api.SimpleResponse
			for {
				var res api.SimpleResponse
				err = stm.Receive(ctx, &res) // Don't create scoped error
				if errors.Is(err, io.EOF) {
					err = nil
					break
				}
				if err != nil {
					t.Logf("Receive returns an error: %s", err)
					break
				}
				ress = append(ress, res)
			}

			stat := status.Convert(err)

			header, err := stm.Header()
			if err != nil {
				t.Fatalf("Header should not return an error, but got '%s'", err)
			}
			if diff := cmp.Diff(c.expectedHeader, header); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			if diff := cmp.Diff(c.expectedTrailer, stm.Trailer()); diff != "" {
				t.Errorf("-want, +got\n%s", diff)
			}
			for i := 0; i < len(c.expectedContent); i++ {
				if diff := cmp.Diff(c.expectedContent[i].String(), ress[i].String()); diff != "" {
					t.Errorf("-want, +got\n%s", diff)
				}
			}
			if stat.Code() != c.expectedStatus.Code() {
				t.Errorf("expected status code: %s, but got %s", c.expectedStatus.Code(), stat.Code())
			}
			if stat.Message() != c.expectedStatus.Message() {
				t.Errorf("expected status message: %s, but got %s", c.expectedStatus.Message(), stat.Message())
			}
		})
	}
}

func injectClientStreamTransport(t *testing.T, tr transport.WebsocketStreamingTransport) {
	old := transport.NewWSStream
	t.Cleanup(func() {
		transport.NewWSStream = old
	})
	transport.NewWSStream = func(string, string, *transport.ConnectOptions) (transport.WebsocketStreamingTransport, error) {
		return tr, nil
	}
}
