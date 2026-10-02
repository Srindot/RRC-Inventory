package main

// A very small MQTT broker that stands in for a printer's status service, so
// the connect loop can be exercised for real over TLS - including the case
// that used to wedge it: a printer rejecting the access code.
//
// Only what paho sends us is handled: CONNECT, SUBSCRIBE, PUBLISH (QoS 0),
// PINGREQ and DISCONNECT. After a successful subscribe it pushes one report,
// as a printer answers "pushall".

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
)

type fakePrinterMQTT struct {
	listener net.Listener
	serial   string

	mu        sync.Mutex
	code      string
	passwords []string // every password tried, in order
	accepted  int
	conns     []net.Conn
}

func newFakePrinterMQTT(t *testing.T, serial, code string) *fakePrinterMQTT {
	t.Helper()

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{selfSignedCert(t)},
	})
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	f := &fakePrinterMQTT{listener: listener, serial: serial, code: code}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conns = append(f.conns, conn)
			f.mu.Unlock()
			go f.serve(conn)
		}
	}()

	t.Cleanup(func() {
		listener.Close()
		f.dropAll()
	})
	return f
}

func (f *fakePrinterMQTT) port() int {
	return f.listener.Addr().(*net.TCPAddr).Port
}

func (f *fakePrinterMQTT) tried() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.passwords...)
}

func (f *fakePrinterMQTT) acceptedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accepted
}

// dropAll hangs up on every client, as a printer does when it reboots.
func (f *fakePrinterMQTT) dropAll() {
	f.mu.Lock()
	conns := f.conns
	f.conns = nil
	f.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

func readRemainingLength(r *bufio.Reader) (int, error) {
	length, multiplier := 0, 1
	for i := 0; i < 4; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		length += int(b&0x7F) * multiplier
		if b&0x80 == 0 {
			return length, nil
		}
		multiplier *= 128
	}
	return length, io.ErrUnexpectedEOF
}

func encodeRemainingLength(n int) []byte {
	var out []byte
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

// mqttString reads one length-prefixed field and returns the rest.
func mqttString(body []byte) (string, []byte) {
	if len(body) < 2 {
		return "", nil
	}
	n := int(binary.BigEndian.Uint16(body))
	if len(body) < 2+n {
		return "", nil
	}
	return string(body[2 : 2+n]), body[2+n:]
}

func (f *fakePrinterMQTT) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		kind, err := reader.ReadByte()
		if err != nil {
			return
		}
		length, err := readRemainingLength(reader)
		if err != nil {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return
		}

		switch kind >> 4 {
		case 1: // CONNECT
			_, rest := mqttString(body) // protocol name
			if len(rest) < 4 {
				return
			}
			flags := rest[1]
			rest = rest[4:]            // level, flags, keepalive
			_, rest = mqttString(rest) // client id
			if flags&0x04 != 0 {
				_, rest = mqttString(rest)
				_, rest = mqttString(rest)
			}
			if flags&0x80 != 0 {
				_, rest = mqttString(rest)
			}
			password := ""
			if flags&0x40 != 0 {
				password, _ = mqttString(rest)
			}

			f.mu.Lock()
			f.passwords = append(f.passwords, password)
			ok := password == f.code
			if ok {
				f.accepted++
			}
			f.mu.Unlock()

			if !ok {
				// 5: not authorised, which is what a printer says to a stale code
				conn.Write([]byte{0x20, 0x02, 0x00, 0x05})
				return
			}
			conn.Write([]byte{0x20, 0x02, 0x00, 0x00})

		case 8: // SUBSCRIBE
			if len(body) < 2 {
				return
			}
			conn.Write([]byte{0x90, 0x03, body[0], body[1], 0x00})

			// Answer as the printer does, with a report
			topic := "device/" + f.serial + "/report"
			payload := []byte(`{"print":{"gcode_state":"IDLE","nozzle_temper":25}}`)
			packet := []byte{byte(len(topic) >> 8), byte(len(topic))}
			packet = append(packet, topic...)
			packet = append(packet, payload...)
			conn.Write(append(append([]byte{0x30}, encodeRemainingLength(len(packet))...), packet...))

		case 3: // PUBLISH from the client, QoS 0 - nothing to acknowledge
		case 12: // PINGREQ
			conn.Write([]byte{0xD0, 0x00})
		case 14: // DISCONNECT
			return
		}
	}
}
