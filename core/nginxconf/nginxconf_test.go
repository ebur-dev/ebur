package nginxconf

import (
	"strings"
	"testing"

	"github.com/ebur/ebur/core/php"
	"github.com/ebur/ebur/core/sites"
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
		Pool: php.Pool{Version: "8.4.28", BasePort: 9100, Size: 2},
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
		Pool: php.Pool{Version: "8.4.28", BasePort: 9100, Size: 1},
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
