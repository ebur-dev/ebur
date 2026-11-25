package nginxconf

import (
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/php"
	"github.com/ebur-dev/ebur/core/project"
	"github.com/ebur-dev/ebur/core/sites"
)

func TestRender(t *testing.T) {
	out := Render(Config{
		NginxDir: `C:\Users\Jane Doe\ebur\bin\nginx\1.30.8`,
		LogDir:   `C:\Users\Jane Doe\ebur\logs`,
		TempDir:  `C:\Users\Jane Doe\ebur\data\nginx-temp`,
		PIDFile:  `C:\Users\Jane Doe\ebur\data\nginx.pid`,
		Listen:   "127.0.0.1:80",
		Sites: []sites.Site{
			{Name: "shop", Host: "shop.test", DocRoot: `C:\Users\Jane Doe\ebur\www\shop\public`},
			{Name: "blog", Host: "blog.test", DocRoot: `C:\Users\Jane Doe\ebur\www\blog`},
		},
		Pools: []php.Pool{{Version: "8.4.28", BasePort: 9100, Size: 2}},
	})
	for _, want := range []string{
		`include "C:/Users/Jane Doe/ebur/bin/nginx/1.30.8/conf/mime.types";`,
		`root "C:/Users/Jane Doe/ebur/www/shop/public";`,
		"server_name shop.test;",
		"server_name blog.test;",
		"upstream php-8_4_28 {",
		"server 127.0.0.1:9101 max_fails=0;",
		"fastcgi_pass php-8_4_28;",
		"listen 127.0.0.1:80 default_server;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(out, "{") != strings.Count(out, "}") {
		t.Error("unbalanced braces")
	}
	if strings.Contains(out, `\U`) {
		t.Error("backslash path leaked into config")
	}
	if strings.Contains(out, "ssl") {
		t.Error("TLS directives without TLS config")
	}
}

// PHP-04: each site goes to the pool of its own PHP version.
func TestRenderPerSitePHP(t *testing.T) {
	out := Render(Config{
		NginxDir: `C:\e\bin\nginx`, LogDir: `C:\e\logs`, TempDir: `C:\e\tmp`, PIDFile: `C:\e\n.pid`,
		Listen: "127.0.0.1:80",
		Sites: []sites.Site{
			{Name: "legacy", Host: "legacy.test", DocRoot: `C:\e\www\legacy`, PHP: project.Choice{Version: "8.3.37"}},
			{Name: "app", Host: "app.test", DocRoot: `C:\e\www\app`, PHP: project.Choice{Version: "8.5.13"}},
			{Name: "plain", Host: "plain.test", DocRoot: `C:\e\www\plain`}, // no choice: default pool
		},
		Pools: []php.Pool{
			{Version: "8.5.13", BasePort: 9100, Size: 2},
			{Version: "8.3.37", BasePort: 9102, Size: 2},
		},
	})
	for _, want := range []string{"upstream php-8_5_13 {", "upstream php-8_3_37 {", "server 127.0.0.1:9103 max_fails=0;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	pass := func(host string) string {
		i := strings.Index(out, "server_name "+host+";")
		j := strings.Index(out[i:], "fastcgi_pass ")
		line := out[i+j:]
		return line[len("fastcgi_pass "):strings.Index(line, ";")]
	}
	if got := pass("legacy.test"); got != "php-8_3_37" {
		t.Errorf("legacy.test -> %s", got)
	}
	if got := pass("app.test"); got != "php-8_5_13" {
		t.Errorf("app.test -> %s", got)
	}
	if got := pass("plain.test"); got != "php-8_5_13" {
		t.Errorf("plain.test should use the default pool, got %s", got)
	}
}

// TOOL-03: mail.test forwards to Mailpit's UI, with WebSocket support.
func TestRenderProxy(t *testing.T) {
	out := Render(Config{
		NginxDir: `C:\e\bin\nginx`, LogDir: `C:\e\logs`, TempDir: `C:\e\tmp`, PIDFile: `C:\e\n.pid`,
		Listen: "127.0.0.1:80", TLSListen: "127.0.0.1:443",
		Pools:   []php.Pool{{Version: "8.5.13", BasePort: 9100, Size: 1}},
		Proxies: []Proxy{{Host: "mail.test", Target: "127.0.0.1:8025"}},
		TLS:     map[string]TLSPair{"mail.test": {Cert: `C:\e\ssl\mail.pem`, Key: `C:\e\ssl\mail-key.pem`}},
	})
	for _, want := range []string{
		"server_name mail.test;",
		"proxy_pass http://127.0.0.1:8025;",
		"proxy_set_header Upgrade $http_upgrade;",
		`ssl_certificate "C:/e/ssl/mail.pem";`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(out, "{") != strings.Count(out, "}") {
		t.Error("unbalanced braces")
	}
}

func TestRenderTLS(t *testing.T) {
	out := Render(Config{
		NginxDir: `C:\e\bin\nginx`, LogDir: `C:\e\logs`, TempDir: `C:\e\tmp`, PIDFile: `C:\e\n.pid`,
		Listen: "127.0.0.1:80", TLSListen: "127.0.0.1:443",
		Sites: []sites.Site{
			{Name: "shop", Host: "shop.test", DocRoot: `C:\e\www\shop`},
			{Name: "plain", Host: "plain.test", DocRoot: `C:\e\www\plain`},
		},
		TLS:     map[string]TLSPair{"shop.test": {Cert: `C:\e\ssl\shop.pem`, Key: `C:\e\ssl\shop-key.pem`}},
		CRLFile: `C:\e\ssl\ca.crl`, CRLPath: "/.well-known/ebur/ca.crl",
		Pools: []php.Pool{{Version: "8.4.28", BasePort: 9100, Size: 1}},
	})
	for _, want := range []string{
		"listen 127.0.0.1:443 ssl default_server;",
		"ssl_reject_handshake on;",
		`ssl_certificate "C:/e/ssl/shop.pem";`,
		"http2 on;",
		"location = /.well-known/ebur/ca.crl {",
		`alias "C:/e/ssl/ca.crl";`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(out, "listen 127.0.0.1:443 ssl;") != 1 {
		t.Error("site without a certificate got a TLS listener")
	}
	if strings.Count(out, "{") != strings.Count(out, "}") {
		t.Error("unbalanced braces")
	}
}
