package collect

import (
	"reflect"
	"testing"

	"github.com/vz-shark/vnetviz/internal/model"
)

func TestParsePorts(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []model.PortMap
	}{
		{"empty", "", nil},
		{
			// Docker emits an IPv4 and an IPv6 binding for the same publish; we
			// keep a single entry per host-port/container-port pair.
			name: "dedup v4/v6",
			in:   "0.0.0.0|8080|80/tcp;::|8080|80/tcp;",
			want: []model.PortMap{{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80/tcp"}},
		},
		{
			name: "multiple distinct",
			in:   "0.0.0.0|8080|80/tcp;0.0.0.0|8443|443/tcp;",
			want: []model.PortMap{
				{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80/tcp"},
				{HostIP: "0.0.0.0", HostPort: "8443", ContainerPort: "443/tcp"},
			},
		},
		{
			// An exposed-but-unpublished port has no host port and is dropped.
			name: "skip unpublished",
			in:   "||6379/tcp;",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePorts(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePorts(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
