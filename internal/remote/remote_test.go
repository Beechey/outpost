package remote

import "testing"

func TestServerValidate(t *testing.T) {
	tests := []struct {
		name string
		srv  Server
		ok   bool
	}{
		{"ip", Server{Addr: "203.0.113.7"}, true},
		{"hostname with port", Server{Addr: "box.local", Port: 2222}, true},
		{"ipv6", Server{Addr: "2001:db8::1", Port: 22}, true},
		{"ssh option injection", Server{Addr: "-oProxyCommand=evil"}, false},
		{"shell in address", Server{Addr: "1.2.3.4;rm"}, false},
		{"command substitution", Server{Addr: "$(reboot)"}, false},
		{"quote", Server{Addr: "a'b"}, false},
		{"port too high", Server{Addr: "1.2.3.4", Port: 70000}, false},
		{"negative port", Server{Addr: "1.2.3.4", Port: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.srv.Validate(); (err == nil) != tt.ok {
				t.Errorf("Validate(%+v) = %v, want ok=%v", tt.srv, err, tt.ok)
			}
		})
	}
}

func TestServerString(t *testing.T) {
	if got := (Server{Addr: "box"}).String(); got != "box" {
		t.Errorf("default port: got %q", got)
	}
	if got := (Server{Addr: "box", Port: 2222}).String(); got != "box:2222" {
		t.Errorf("custom port: got %q", got)
	}
}
