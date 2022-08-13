// Copyright 2022 Ian Gudger.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package memipnet

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"golang.org/x/net/nettest"
	"golang.org/x/sync/errgroup"
)

func TestTCPListenDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := NewStack()
	if err != nil {
		t.Fatal("NewStack:", err)
	}
	defer s.Close()

	type addrNet struct {
		network string
		address string
	}
	for i, listen := range []struct {
		addrNet
		dial []addrNet
	}{
		{addrNet{"tcp", "localhost"}, []addrNet{
			{"tcp", "localhost"},
			{"tcp6", "localhost"},
			{"tcp", "[::1]"},
			{"tcp6", "[::1]"},
		}},
		{addrNet{"tcp4", "localhost"}, []addrNet{
			{"tcp", "localhost"},
			{"tcp", "127.0.0.1"},
			{"tcp4", "127.0.0.1"},
		}},
		{addrNet{"tcp6", "localhost"}, []addrNet{
			{"tcp", "localhost"},
			{"tcp6", "localhost"},
			{"tcp", "[::1]"},
			{"tcp6", "[::1]"},
		}},
		{addrNet{"tcp", "127.0.0.1"}, []addrNet{
			{"tcp", "localhost"},
			{"tcp", "127.0.0.1"},
			{"tcp4", "127.0.0.1"},
		}},
		{addrNet{"tcp4", "127.0.0.1"}, []addrNet{
			{"tcp", "localhost"},
			{"tcp", "127.0.0.1"},
			{"tcp4", "127.0.0.1"},
		}},
		{addrNet{"tcp", "[::1]"}, []addrNet{
			{"tcp", "localhost"},
			{"tcp6", "localhost"},
			{"tcp", "[::1]"},
			{"tcp6", "[::1]"},
		}},
		{addrNet{"tcp6", "[::1]"}, []addrNet{
			{"tcp", "localhost"},
			{"tcp6", "localhost"},
			{"tcp", "[::1]"},
			{"tcp6", "[::1]"},
		}},
		{addrNet{"tcp", "127.0.1.1"}, []addrNet{
			{"tcp", "127.0.1.1"},
			{"tcp4", "127.0.1.1"},
		}},
		{addrNet{"tcp4", "127.0.1.1"}, []addrNet{
			{"tcp", "127.0.1.1"},
			{"tcp4", "127.0.1.1"},
		}},
	} {
		listenAddr := fmt.Sprintf("%s:%d", listen.address, i+1001)
		t.Run(fmt.Sprintf("Listen(%s, %s)", listen.network, listenAddr), func(t *testing.T) {
			l, err := s.Listen(ctx, listen.network, listenAddr)
			if err != nil {
				t.Fatal("Listen:", err)
			}
			defer l.Close()
			for _, dial := range listen.dial {
				dialAddr := fmt.Sprintf("%s:%d", dial.address, i+1001)
				t.Run(fmt.Sprintf("Dial(%s, %s)", dial.network, dialAddr), func(t *testing.T) {
					nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
						c1, err = s.DialContext(ctx, dial.network, dialAddr)
						if err != nil {
							return nil, nil, nil, fmt.Errorf("DialContext: %w", err)
						}
						c2, err = l.Accept()
						if err != nil {
							c1.Close()
							return nil, nil, nil, fmt.Errorf("Accept: %w", err)
						}
						stop = func() {
							c1.Close()
							c2.Close()
						}
						return
					})
				})
			}
		})
	}
}

func TestNetHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const want = "hello world"

	s, err := NewStack()
	if err != nil {
		t.Fatal("NewStack:", err)
	}
	defer s.Close()

	l, err := s.Listen(ctx, "tcp", "127.0.0.1:80")
	if err != nil {
		t.Fatal(`Listen("tcp", "127.0.0.1:80"):`, err)
	}
	defer l.Close()

	server := http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, err := writer.Write([]byte(want)); err != nil {
			t.Error("http.ResponseWriter.Write:", err)
		}
	})}

	var eg errgroup.Group
	eg.Go(func() error {
		return server.Serve(l)
	})

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = s.DialContext
	client := http.Client{Transport: transport}

	response, err := client.Get(fmt.Sprintf("http://%s/", l.Addr()))
	if err != nil {
		t.Fatal("http.Client.Get:", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Errorf("got http.Response.StatusCode = %d, want = %d", response.StatusCode, http.StatusOK)
	}
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal("io.ReadAll(http.Response.Body):", err)
	}
	if got := string(got); got != want {
		t.Errorf("got http.Response.Body = %q, want = %q", got, want)
	}

	if err := server.Shutdown(ctx); err != nil {
		t.Fatal("http.Server.Shutdown:", err)
	}

	if err := eg.Wait(); err != http.ErrServerClosed {
		t.Errorf("got http.Server.Serve = %v, want = %v", err, http.ErrServerClosed)
	}
}
