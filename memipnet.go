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

// Package memipnet provides hermetic net package TCP and UDP loopback implementations.
package memipnet

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/loopback"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

// A Stack is a hermetic network stack emulator.
type Stack struct {
	stack *stack.Stack
}

const (
	nicID        = 1
	ipv4Loopback = "\x7f\x00\x00\x01"
	ipv6Loopback = "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01"
)

// NewStack creates a new hermetic network stack emulator.
func NewStack() (*Stack, error) {
	// Create the stack and add a NIC.
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	if err := s.CreateNIC(nicID, loopback.New()); err != nil {
		return nil, fmt.Errorf("CreateNIC: %s", err)
	}

	// Add default routes.
	s.SetRouteTable([]tcpip.Route{{
		Destination: header.IPv4EmptySubnet,
		NIC:         nicID,
	}, {
		Destination: header.IPv6EmptySubnet,
		NIC:         nicID,
	}})

	// Add loopbacks.
	if err := s.AddProtocolAddress(nicID, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   ipv4Loopback,
			PrefixLen: 8,
		},
	}, stack.AddressProperties{}); err != nil {
		return nil, fmt.Errorf("AddProtocolAddress(127.0.0.1): %s", err)
	}
	if err := s.AddProtocolAddress(nicID, tcpip.ProtocolAddress{
		Protocol: ipv6.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   ipv6Loopback,
			PrefixLen: 128,
		},
	}, stack.AddressProperties{}); err != nil {
		return nil, fmt.Errorf("AddProtocolAddress(::1): %s", err)
	}

	return &Stack{s}, nil
}

// Close releases all resources owned by this Stack.
//
// Close does no prevent additional uses of this Stack.
func (s *Stack) Close() {
	s.stack.Close()
	s.stack.Wait()
}

// Listen announces on the emulated local network address.
//
// See net.Listen for a description of the network and address parameters.
func (s *Stack) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	protocol, ips, port, err := parseProtocolIPPort("tcp", network, address)
	if err != nil {
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: nil, Err: err}
	}

	var ip net.IP
	for _, ip = range ips {
		var l net.Listener
		l, err = gonet.ListenTCP(s.stack, tcpip.FullAddress{
			NIC:  nicID,
			Addr: tcpip.Address(ip),
			Port: port,
		}, protocol)
		if err == nil {
			return l, nil
		}
	}
	return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: &net.TCPAddr{IP: ip, Port: int(port)}, Err: fmt.Errorf("%s", err)}
}

// ListenPacket announces on the emulated local network address.
//
// See net.ListenPacket for a description of the network and address parameters.
func (s *Stack) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	protocol, ips, port, err := parseProtocolIPPort("udp", network, address)
	if err != nil {
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: nil, Err: err}
	}

	var ip net.IP
	for _, ip = range ips {
		var c net.PacketConn
		c, err = gonet.DialUDP(s.stack, &tcpip.FullAddress{
			NIC:  nicID,
			Addr: tcpip.Address(ip),
			Port: port,
		}, nil, protocol)
		if err == nil {
			return c, nil
		}
	}
	return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: &net.TCPAddr{IP: ip, Port: int(port)}, Err: fmt.Errorf("%s", err)}
}

func parseProtocolIPPort(prefix, network, address string) (tcpip.NetworkProtocolNumber, []net.IP, uint16, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return 0, nil, 0, err
	}
	portNum, err := strconv.ParseInt(port, 10, 0)
	if err != nil || 0 > portNum || portNum > 65535 {
		return 0, nil, 0, &net.AddrError{Err: "invalid port", Addr: address}
	}

	switch network {
	case prefix + "4":
		if host == "localhost" {
			host = "127.0.0.1"
		}
		ip := net.ParseIP(host).To4()
		if ip == nil {
			return 0, nil, 0, &net.AddrError{Err: "invalid IPv4 address", Addr: address}
		}
		return ipv4.ProtocolNumber, []net.IP{ip}, uint16(portNum), nil

	case prefix:
		if host == "localhost" {
			return ipv6.ProtocolNumber, []net.IP{
				net.IP(ipv6Loopback).To16(),
				net.IP(ipv4Loopback).To16(),
			}, uint16(portNum), nil
		}
		fallthrough

	case prefix + "6":
		if host == "localhost" {
			host = "::1"
		}
		ip := net.ParseIP(host).To16()
		if ip == nil {
			return 0, nil, 0, &net.AddrError{Err: "invalid IP address", Addr: address}
		}
		return ipv6.ProtocolNumber, []net.IP{ip}, uint16(portNum), nil

	default:
		return 0, nil, 0, net.UnknownNetworkError(network)
	}
}

// Dial connects to the address on the named network.
//
// See net.Dial for a description of the network and address parameters.
func (s *Stack) Dial(network, address string) (net.Conn, error) {
	return s.DialContext(context.Background(), network, address)
}

// DialContext connects to the address on the named network using the provided context.
//
// See net.Dial for a description of the network and address parameters.
func (s *Stack) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	protocol, ips, port, err := parseProtocolIPPort("tcp", network, address)

	if err != nil {
		if _, ok := err.(net.UnknownNetworkError); !ok {
			return nil, &net.OpError{Op: "dial", Net: network, Source: nil, Addr: nil, Err: err}
		}
	}
	if err == nil {
		var (
			ip  net.IP
			err error
		)
		for _, ip = range ips {
			var c net.Conn
			c, err = gonet.DialContextTCP(ctx, s.stack, tcpip.FullAddress{
				NIC:  nicID,
				Addr: tcpip.Address(ip),
				Port: port,
			}, protocol)
			if err == nil {
				return c, nil
			}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Source: nil, Addr: &net.TCPAddr{IP: ip, Port: int(port)}, Err: err}
	}
	protocol, ips, port, err = parseProtocolIPPort("udp", network, address)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Source: nil, Addr: nil, Err: err}
	}

	var ip net.IP
	for _, ip = range ips {
		var c net.Conn
		c, err = gonet.DialUDP(s.stack, nil, &tcpip.FullAddress{
			NIC:  nicID,
			Addr: tcpip.Address(ip),
			Port: port,
		}, protocol)
		if err == nil {
			return c, nil
		}
	}
	return nil, &net.OpError{Op: "dial", Net: network, Source: nil, Addr: &net.TCPAddr{IP: ip, Port: int(port)}, Err: err}
}
