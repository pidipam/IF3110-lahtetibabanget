package dbconn

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func validInput() Input {
	return Input{Name: "target", Host: "127.0.0.1", Port: 5435, DBName: "sample", Username: "app", Password: "pw", SSLMode: "disable"}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
		reqPw  bool
		ok     bool
	}{
		{"valid", func(*Input) {}, true, true},
		{"nama kosong", func(i *Input) { i.Name = "" }, true, false},
		{"nama terlalu panjang", func(i *Input) { i.Name = strings.Repeat("a", 65) }, true, false},
		{"host kosong", func(i *Input) { i.Host = "" }, true, false},
		{"host berisi spasi", func(i *Input) { i.Host = "a b" }, true, false},
		{"host berisi @", func(i *Input) { i.Host = "evil@host" }, true, false},
		{"port 0", func(i *Input) { i.Port = 0 }, true, false},
		{"port negatif", func(i *Input) { i.Port = -1 }, true, false},
		{"port terlalu besar", func(i *Input) { i.Port = 65536 }, true, false},
		{"db kosong", func(i *Input) { i.DBName = "" }, true, false},
		{"user kosong", func(i *Input) { i.Username = "" }, true, false},
		{"password kosong saat create", func(i *Input) { i.Password = "" }, true, false},
		{"password kosong saat update", func(i *Input) { i.Password = "" }, false, true},
		{"sslmode aneh", func(i *Input) { i.SSLMode = "bogus" }, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mutate(&in)
			err := in.Validate(tc.reqPw)
			if tc.ok && err != nil {
				t.Fatalf("seharusnya valid: %v", err)
			}
			var ve *ValidationError
			if !tc.ok && !errors.As(err, &ve) {
				t.Fatalf("seharusnya ValidationError, dapat %v", err)
			}
		})
	}
}

func TestNormalizeDefaultsSSLMode(t *testing.T) {
	in := validInput()
	in.SSLMode = "  "
	in.Name = "  x  "
	in.Normalize()
	if in.SSLMode != "prefer" || in.Name != "x" {
		t.Fatalf("normalize salah: %+v", in)
	}
}

func TestDSNEscapesSpecialCharacters(t *testing.T) {
	p := Params{Host: "db.local", Port: 5432, DBName: "my db", Username: "us@er", Password: "p@ss:w/rd?#%", SSLMode: "require"}
	u, err := url.Parse(p.DSN())
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u.User.Password()
	if u.User.Username() != "us@er" || pw != "p@ss:w/rd?#%" {
		t.Fatalf("kredensial rusak setelah parse: %q %q", u.User.Username(), pw)
	}
	if u.Host != "db.local:5432" || u.Path != "/my db" || u.Query().Get("sslmode") != "require" {
		t.Fatalf("DSN salah: %s", p.DSN())
	}
}

func TestDSNIPv6Host(t *testing.T) {
	p := Params{Host: "::1", Port: 5432, DBName: "d", Username: "u", Password: "p"}
	if !strings.Contains(p.DSN(), "[::1]:5432") {
		t.Fatalf("IPv6 harus di dalam kurung: %s", p.DSN())
	}
}

func TestParamsNeverPrintsPassword(t *testing.T) {
	p := Params{Host: "h", Port: 1, DBName: "d", Username: "u", Password: "topsecret"}
	for _, s := range []string{fmt.Sprint(p), fmt.Sprintf("%v", p), fmt.Sprintf("%+v", p), fmt.Sprintf("%#v", p)} {
		if strings.Contains(s, "topsecret") {
			t.Fatalf("password bocor: %s", s)
		}
	}
}

func TestConnErrorIsAndGeneric(t *testing.T) {
	raw := errors.New("dial tcp 10.1.2.3:5432: secret detail")
	err := error(&ConnError{Kind: KindRefused, err: raw})
	if !errors.Is(err, ErrConnectionFailed) || !errors.Is(err, raw) {
		t.Fatal("errors.Is harus cocok ke ErrConnectionFailed dan penyebab asli")
	}
	if strings.Contains(err.Error(), "10.1.2.3") {
		t.Fatalf("Error() membocorkan detail internal: %s", err)
	}
	for k := KindOther; k <= KindHost; k++ {
		if k.UserMessage() == "" {
			t.Errorf("kind %v tanpa pesan", k)
		}
	}
}
