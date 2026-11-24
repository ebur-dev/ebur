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
}
