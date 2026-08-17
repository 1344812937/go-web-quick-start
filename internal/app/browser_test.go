package app

import (
	"errors"
	"io"
	"net"
	"reflect"
	"syscall"
	"testing"
)

func TestLocalBrowserURL(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{name: "wildcard IPv4", host: "0.0.0.0", port: "8888", want: "http://127.0.0.1:8888/static/"},
		{name: "wildcard IPv6", host: "::", port: "9000", want: "http://[::1]:9000/static/"},
		{name: "specific host", host: "192.168.1.20", port: "8080", want: "http://192.168.1.20:8080/static/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := localBrowserURL(test.host, test.port); got != test.want {
				t.Fatalf("localBrowserURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDefaultBrowserCommand(t *testing.T) {
	const targetURL = "http://127.0.0.1:8888/static/"
	tests := []struct {
		goos      string
		command   string
		args      []string
		wantError bool
	}{
		{goos: "darwin", command: "open", args: []string{targetURL}},
		{goos: "windows", command: "rundll32", args: []string{"url.dll,FileProtocolHandler", targetURL}},
		{goos: "linux", command: "xdg-open", args: []string{targetURL}},
		{goos: "plan9", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			command, args, err := defaultBrowserCommand(test.goos, targetURL)
			if test.wantError {
				if err == nil {
					t.Fatal("defaultBrowserCommand() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultBrowserCommand() error = %v", err)
			}
			if command != test.command || !reflect.DeepEqual(args, test.args) {
				t.Fatalf("defaultBrowserCommand() = %q, %v; want %q, %v", command, args, test.command, test.args)
			}
		})
	}
}

func TestOpenBrowserOnce(t *testing.T) {
	originalOutput := log.Out
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
	})

	openCount := 0
	manager := &AppWebManager{
		openBrowser: true,
		browserOpener: func(string) error {
			openCount++
			return nil
		},
	}

	manager.openBrowserOnce("http://127.0.0.1:8888/static/")
	manager.openBrowserOnce("http://127.0.0.1:8888/static/")

	if openCount != 1 {
		t.Fatalf("browser opened %d times, want 1", openCount)
	}
}

func TestOpenBrowserOnceDisabled(t *testing.T) {
	openCount := 0
	manager := &AppWebManager{
		openBrowser: false,
		browserOpener: func(string) error {
			openCount++
			return nil
		},
	}

	manager.openBrowserOnce("http://127.0.0.1:8888/static/")

	if openCount != 0 {
		t.Fatalf("browser opened %d times, want 0", openCount)
	}
}

func TestListenWithPortFallbackUsesAvailablePortWhenPreferredPortIsBusy(t *testing.T) {
	var addresses []string
	listener, actualPort, err := listenWithPortFallbackUsing(func(network string, address string) (net.Listener, error) {
		addresses = append(addresses, address)
		if len(addresses) == 1 {
			return nil, &net.OpError{Op: "listen", Net: network, Addr: &testAddr{network: network, address: address}, Err: syscall.EADDRINUSE}
		}
		return &testListener{address: testAddr{network: network, address: "127.0.0.1:40123"}}, nil
	}, "127.0.0.1", "8888")
	if err != nil {
		t.Fatalf("listenWithPortFallback() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	if !reflect.DeepEqual(addresses, []string{"127.0.0.1:8888", "127.0.0.1:0"}) {
		t.Fatalf("listen addresses = %#v", addresses)
	}
	if actualPort != "40123" {
		t.Fatalf("fallback port = %s, want 40123", actualPort)
	}
}

type testAddr struct {
	network string
	address string
}

func (a testAddr) Network() string { return a.network }
func (a testAddr) String() string  { return a.address }

type testListener struct {
	address net.Addr
}

func (l *testListener) Accept() (net.Conn, error) {
	return nil, errors.New("test listener does not accept connections")
}
func (l *testListener) Close() error   { return nil }
func (l *testListener) Addr() net.Addr { return l.address }
