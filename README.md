# Hermetic Go `net` package TCP and UDP loopback implementations [![GoDoc](https://godoc.org/github.com/iangudger/memipnet?status.png)](https://godoc.org/github.com/iangudger/memipnet)

Allows fully hermetic testing of code which depends on TCP and/or UDP loopback networking. Useful for all cases where a TCP or UDP loopback sockets would be used and all use is confined to a single process and goes through the `net` package.

This is a more compatible, but heavier weight alternative to [`memnet`](https://github.com/iangudger/memnet).

This package uses [gVisor](https://gvisor.dev)'s [pure Go network stack](https://cs.opensource.google/gvisor/gvisor/+/master:pkg/tcpip/).
