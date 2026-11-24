package php

import (
	"strconv"
	"strings"
	"testing"
)

func TestSpecsStaggered(t *testing.T) {
	p := Pool{Version: "8.4.28", Dir: `C:\ebur\bin\php\8.4.28`, INI: `C:\ebur\etc\php\8.4.28\php.ini`, BasePort: 9840, Size: 4, MaxRequests: 100, LogDir: `C:\ebur\logs`}
	specs, err := p.Specs()
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("got %d specs", len(specs))
	}
	seen := map[string]bool{}
	for i, s := range specs {
		limit := s.Env[0]
		if seen[limit] {
			t.Errorf("duplicate limit %s", limit)
		}
		seen[limit] = true
		if !strings.HasSuffix(s.Path, "php-cgi.exe") || s.Args[1] != "127.0.0.1:"+strconv.Itoa(9840+i) || s.Args[3] != p.INI {
			t.Errorf("spec %d: %+v", i, s)
		}
		if s.StableAfter == 0 || !strings.HasSuffix(s.LogFile, "php-8.4.28.log") {
			t.Errorf("spec %d: %+v", i, s)
		}
	}
	if specs[0].Env[0] != "PHP_FCGI_MAX_REQUESTS=100" || specs[3].Env[0] != "PHP_FCGI_MAX_REQUESTS=175" {
		t.Errorf("limits: %s .. %s", specs[0].Env[0], specs[3].Env[0])
	}
	if specs[0].Name != "php-8.4.28#1" {
		t.Error(specs[0].Name)
	}
}

func TestDefaultsAndValidation(t *testing.T) {
	specs, err := Pool{Version: "8.3.37", Dir: "x", BasePort: 9830, Size: 1}.Specs()
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Env[0] != "PHP_FCGI_MAX_REQUESTS=500" || len(specs[0].Args) != 2 || specs[0].LogFile != "" {
		t.Errorf("%+v", specs[0])
	}
	for _, bad := range []Pool{{Size: 0, BasePort: 9000}, {Size: 65, BasePort: 9000}, {Size: 2, BasePort: 80}, {Size: 4, BasePort: 65534}} {
		if _, err := bad.Specs(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestNginxUpstream(t *testing.T) {
	got := Pool{Version: "8.4.28", BasePort: 9840, Size: 2}.NginxUpstream()
	want := "upstream php-8_4_28 {\n    least_conn;\n    server 127.0.0.1:9840 max_fails=0;\n    server 127.0.0.1:9841 max_fails=0;\n}\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
