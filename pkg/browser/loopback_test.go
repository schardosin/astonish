package browser

import "testing"

func TestNormalizeLoopbackURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "localhost http",
			in:   "http://localhost:3001/automation/create",
			want: "http://127.0.0.1:3001/automation/create",
		},
		{
			name: "localhost https",
			in:   "https://localhost/path",
			want: "https://127.0.0.1/path",
		},
		{
			name: "ipv6 loopback bracketed",
			in:   "http://[::1]:3001/",
			want: "http://127.0.0.1:3001/",
		},
		{
			name: "already ipv4 loopback",
			in:   "http://127.0.0.1:3001/",
			want: "http://127.0.0.1:3001/",
		},
		{
			name: "non-loopback unchanged",
			in:   "http://10.135.224.227:3001/",
			want: "http://10.135.224.227:3001/",
		},
		{
			name: "external unchanged",
			in:   "https://example.com/x",
			want: "https://example.com/x",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeLoopbackURL(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeLoopbackURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
