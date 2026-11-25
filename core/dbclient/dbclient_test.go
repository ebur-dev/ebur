package dbclient

import (
	"errors"
	"testing"
)

func TestFirstLine(t *testing.T) {
	fallback := errors.New("exit status 1")
	cases := map[string]string{
		"": "exit status 1",
		"WARNING: option --ssl-verify-server-cert is disabled\nERROR 1007 (HY000): Can't create database 'x'": "ERROR 1007 (HY000): Can't create database 'x'",
		"--------------\nTHIS IS NOT SQL\n--------------\n\nERROR 1064 (42000) at line 3: You have an error":  "ERROR 1064 (42000) at line 3: You have an error",
		"WARNING: x\nmariadb-dump: Got error: 1049":                                                           "mariadb-dump: Got error: 1049",
	}
	for in, want := range cases {
		if got := FirstLine(in, fallback); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	for _, ok := range []string{"shop", "lara_demo", "my-shop", "a$b"} {
		if !ValidName(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a`b", "../x", "a;drop", string(make([]byte, 65))} {
		if ValidName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if !IsSystem("MySQL") || !IsSystem("information_schema") || IsSystem("shop") {
		t.Error("system names")
	}
}
