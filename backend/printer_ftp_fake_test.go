package main

// A very small FTP server that stands in for a printer, so the upload path can
// be exercised for real - control connection, data connection, the lot - and
// made to misbehave in the specific ways a P1S does.
//
// Only the commands this codebase issues are implemented: USER, PASS, TYPE,
// PASV, STOR, LIST, DELE, SIZE, QUIT.

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakePrinterFTP is a stand-in printer. The zero value is a well behaved one.
type fakePrinterFTP struct {
	listener net.Listener

	// withholdShutStatus skips the "226 closing data connection" reply after a
	// STOR, reproducing a printer whose control connection has gone away. This
	// is what made uploads sit at 100% forever.
	withholdShutStatus bool

	// truncateAfter, when positive, stores only that many bytes and drops the
	// rest, reproducing a transfer that dies part way.
	truncateAfter int

	mu      sync.Mutex
	files   map[string][]byte
	deletes []string
}

func newFakePrinterFTP(t *testing.T) *fakePrinterFTP {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	f := &fakePrinterFTP{listener: listener, files: map[string][]byte{}}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()

	t.Cleanup(func() { listener.Close() })
	return f
}

func (f *fakePrinterFTP) port() int {
	return f.listener.Addr().(*net.TCPAddr).Port
}

func (f *fakePrinterFTP) stored(name string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.files[name]
	return b, ok
}

func (f *fakePrinterFTP) deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

func (f *fakePrinterFTP) serve(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	write := func(format string, args ...any) {
		fmt.Fprintf(conn, format+"\r\n", args...)
	}

	write("220 fake printer ready")

	var dataListener net.Listener
	defer func() {
		if dataListener != nil {
			dataListener.Close()
		}
	}()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "USER":
			write("331 need password")
		case "PASS":
			write("230 logged in")
		case "TYPE":
			write("200 type set")
		case "PASV":
			dataListener, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				write("425 cannot open data connection")
				continue
			}
			port := dataListener.Addr().(*net.TCPAddr).Port
			write("227 entering passive mode (127,0,0,1,%d,%d)", port/256, port%256)
		case "STOR":
			f.handleStor(arg, dataListener, write)
			dataListener = nil
		case "LIST":
			f.handleList(dataListener, write)
			dataListener = nil
		case "SIZE":
			f.mu.Lock()
			body, ok := f.files[arg]
			f.mu.Unlock()
			if !ok {
				write("550 not found")
				continue
			}
			write("213 %d", len(body))
		case "DELE":
			f.mu.Lock()
			delete(f.files, arg)
			f.deletes = append(f.deletes, arg)
			f.mu.Unlock()
			write("250 deleted")
		case "QUIT":
			write("221 bye")
			return
		default:
			write("500 unknown command")
		}
	}
}

func (f *fakePrinterFTP) handleStor(name string, dataListener net.Listener, write func(string, ...any)) {
	if dataListener == nil {
		write("425 no data connection")
		return
	}
	defer dataListener.Close()

	write("150 ready for data")

	dataConn, err := dataListener.Accept()
	if err != nil {
		return
	}

	var body []byte
	if f.truncateAfter > 0 {
		body = make([]byte, f.truncateAfter)
		read, _ := io.ReadFull(dataConn, body)
		body = body[:read]
		// Hang up early, exactly like a transfer that dies part way
		dataConn.Close()
	} else {
		body, _ = io.ReadAll(dataConn)
		dataConn.Close()
	}

	f.mu.Lock()
	f.files[name] = body
	f.mu.Unlock()

	// A printer whose control connection has timed out never sends this, and a
	// client without a shut timeout waits for it forever.
	if f.withholdShutStatus {
		return
	}
	write("226 transfer complete")
}

func (f *fakePrinterFTP) handleList(dataListener net.Listener, write func(string, ...any)) {
	if dataListener == nil {
		write("425 no data connection")
		return
	}
	defer dataListener.Close()

	write("150 here comes the listing")

	dataConn, err := dataListener.Accept()
	if err != nil {
		return
	}

	f.mu.Lock()
	for name, body := range f.files {
		fmt.Fprintf(dataConn, "-rw-r--r-- 1 root root %d Jan 01 00:00 %s\r\n", len(body), name)
	}
	f.mu.Unlock()

	dataConn.Close()
	write("226 listing done")
}
