// Spike: from nothing to https://spike.test with no browser warning,
// covering the hosts helper (SITE-02), mkcert (SSL-01..03) and Nginx TLS.
//
// Usage:
//
//	https setup     install local CA (Windows confirmation), issue cert, add hosts entry (UAC)
//	https verify    start Nginx on 443, check TLS with Go, curl and headless Edge, stop
//	https cleanup   remove hosts entry (UAC), uninstall CA (Windows confirmation), delete keys
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const site = "spike.test"

var (
	dir      string
	runtimes string
	sslDir   string
	workDir  string
	nginxDir string
	mkcert   string
)

func main() {
	log.SetFlags(log.Ltime)
	dir, _ = os.Getwd()
	runtimes = filepath.Join(dir, "..", "runtimes")
	sslDir = filepath.Join(dir, "ssl")
	workDir = filepath.Join(dir, "work")
	nginxDir = filepath.Join(runtimes, "nginx", "nginx-1.30.8")
	mkcert = filepath.Join(runtimes, "mkcert", "mkcert.exe")
	os.Setenv("CAROOT", sslDir) // keep the CA inside the Ebur root, not %LOCALAPPDATA%

	if len(os.Args) != 2 {
		log.Fatal("usage: https setup|verify|cleanup")
	}
	var err error
	switch os.Args[1] {
	case "setup":
		err = setup()
	case "verify":
		err = verify(false)
	case "verify-nohosts":
		// Each client maps spike.test to 127.0.0.1 itself, for machines where
		// the hosts file can't be edited. Proves the TLS chain only.
		err = verify(true)
	case "cleanup":
		err = cleanup()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		log.Fatal(err)
	}
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if s := strings.TrimSpace(string(out)); s != "" {
		fmt.Println(indent(s))
	}
	return err
}

func indent(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }

func timed(label string, f func() error) error {
	t := time.Now()
	err := f()
	log.Printf("%s: %v (err=%v)", label, time.Since(t).Round(time.Millisecond), err)
	return err
}

func setup() error {
	if err := os.MkdirAll(sslDir, 0o700); err != nil {
		return err
	}
	if err := timed("install local CA (includes time on the Windows dialog)", func() error {
		return run(mkcert, "-install")
	}); err != nil {
		return err
	}
	if err := timed("issue site certificate", func() error {
		return run(mkcert, "-cert-file", filepath.Join(sslDir, site+".pem"),
			"-key-file", filepath.Join(sslDir, site+"-key.pem"), site)
	}); err != nil {
		return err
	}
	return timed("hosts entry via elevated helper (includes time on UAC prompt)", func() error {
		return helper("hosts-apply", site)
	})
}

func cleanup() error {
	err1 := timed("remove hosts entry", func() error { return helper("hosts-clear", "") })
	err2 := timed("uninstall local CA", func() error { return run(mkcert, "-uninstall") })
	if err1 != nil || err2 != nil {
		return fmt.Errorf("hosts: %v, ca: %v (keys kept so you can retry)", err1, err2)
	}
	return os.RemoveAll(sslDir)
}

// ---- elevation: ShellExecuteEx with the "runas" verb shows UAC ----

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	errorCancelled        = syscall.Errno(1223)
)

var procShellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

func runElevated(exe, params string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString(params)
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: args,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, err := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if err == errorCancelled {
			return fmt.Errorf("user declined the admin prompt")
		}
		return err
	}
	defer windows.CloseHandle(info.hProcess)
	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	windows.GetExitCodeProcess(info.hProcess, &code)
	if code != 0 {
		return fmt.Errorf("helper exit code %d", code)
	}
	return nil
}

func helper(cmd, hosts string) error {
	tmp, err := os.MkdirTemp("", "ebur-helper")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	result := filepath.Join(tmp, "result.txt")
	params := fmt.Sprintf("%s %q", cmd, result)
	if cmd == "hosts-apply" {
		list := filepath.Join(tmp, "hosts.txt")
		if err := os.WriteFile(list, []byte(hosts+"\n"), 0o644); err != nil {
			return err
		}
		params = fmt.Sprintf("%s %q %q", cmd, list, result)
	}
	err = runElevated(filepath.Join(dir, "ebur-helper.exe"), params)
	if b, rerr := os.ReadFile(result); rerr == nil {
		log.Printf("helper says: %s", b)
	}
	return err
}

// ---- verify: Nginx with TLS, then three independent clients ----

func verify(mapLocal bool) error {
	var curlExtra, edgeExtra []string
	if mapLocal {
		http.DefaultTransport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, _ := net.SplitHostPort(addr)
			return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:"+port)
		}
		curlExtra = []string{"--resolve", site + ":443:127.0.0.1"}
		edgeExtra = []string{"--host-resolver-rules=MAP " + site + " 127.0.0.1"}
		fmt.Println("(name resolution overridden per client; hosts file not used)")
	}
	www := filepath.Join(workDir, "www")
	for _, d := range []string{www, filepath.Join(workDir, "conf"), filepath.Join(workDir, "logs"), filepath.Join(workDir, "temp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	page := `<!doctype html><html><head><meta charset="utf-8"><title>spike.test</title></head>
<body style="margin:0;font-family:Segoe UI,sans-serif;background:#F6F1E4;color:#2B2F33">
<div style="background:#2B2F33;color:#F6F1E4;padding:20px 24px;font-size:22px;font-weight:600">Ebur</div>
<p style="padding:24px;font-size:18px">&#x1F512; Served over HTTPS by Nginx with a local mkcert certificate.</p></body></html>`
	if err := os.WriteFile(filepath.Join(www, "index.html"), []byte(page), 0o644); err != nil {
		return err
	}
	fwd := filepath.ToSlash
	conf := fmt.Sprintf(`worker_processes 1;
error_log logs/error.log warn;
pid logs/nginx.pid;
events { worker_connections 64; }
http {
  access_log off;
  client_body_temp_path temp/body; fastcgi_temp_path temp/fastcgi; proxy_temp_path temp/proxy;
  uwsgi_temp_path temp/uwsgi; scgi_temp_path temp/scgi;
  server { listen 127.0.0.1:80; server_name %[1]s; return 301 https://$host$request_uri; }
  server {
    listen 127.0.0.1:443 ssl;
    http2 on;
    server_name %[1]s;
    ssl_certificate "%[2]s";
    ssl_certificate_key "%[3]s";
    root "%[4]s";
    default_type text/html;
  }
}
`, site, fwd(filepath.Join(sslDir, site+".pem")), fwd(filepath.Join(sslDir, site+"-key.pem")), fwd(www))
	if err := os.WriteFile(filepath.Join(workDir, "conf", "nginx.conf"), []byte(conf), 0o644); err != nil {
		return err
	}
	ng := func(args ...string) *exec.Cmd {
		c := exec.Command(filepath.Join(nginxDir, "nginx.exe"), append([]string{"-p", fwd(workDir) + "/", "-c", "conf/nginx.conf"}, args...)...)
		c.SysProcAttr = &windows.SysProcAttr{HideWindow: true}
		return c
	}
	if out, err := ng("-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx -t: %v\n%s", err, out)
	}
	nginx := ng()
	if err := nginx.Start(); err != nil {
		return err
	}
	defer func() {
		ng("-s", "quit").Run()
		nginx.Wait()
		log.Printf("nginx stopped")
	}()
	time.Sleep(500 * time.Millisecond)

	ok := true
	report := func(name string, err error, detail string) {
		mark := "PASS"
		if err != nil {
			mark, ok = "FAIL", false
			detail = err.Error()
		}
		fmt.Printf("[%s] %s: %s\n", mark, name, detail)
	}

	// 1. Go's TLS stack, which on Windows verifies through the OS certificate store.
	resp, err := http.Get("https://" + site + "/")
	detail := ""
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		st := resp.TLS
		detail = fmt.Sprintf("%s, %s, issuer %q", resp.Proto, tls.VersionName(st.Version), st.PeerCertificates[0].Issuer.CommonName)
	}
	report("Go client (OS trust store)", err, detail)

	// 2. Windows curl (Schannel), no -k.
	curlArgs := append(curlExtra, "-sS", "-o", "NUL", "-w", "%{http_code} %{ssl_verify_result}", "https://"+site+"/")
	out, err := exec.Command("curl.exe", curlArgs...).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v: %s", err, out)
	}
	report("curl.exe / Schannel", err, "http_code + verify result: "+strings.TrimSpace(string(out)))

	// 3. HTTP redirects to HTTPS.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r2, err := noRedirect.Get("http://" + site + "/")
	if err == nil {
		r2.Body.Close()
		if r2.StatusCode != 301 {
			err = fmt.Errorf("status %d", r2.StatusCode)
		}
		detail = "301 -> " + r2.Header.Get("Location")
	}
	report("http -> https redirect", err, detail)

	// 4. A real browser engine: headless Edge screenshot. A certificate
	//    problem would render Edge's warning page instead of the site.
	edge := filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe")
	shot := filepath.Join(workDir, "edge.png")
	os.Remove(shot)
	profile, _ := os.MkdirTemp("", "ebur-edge")
	defer os.RemoveAll(profile)
	edgeArgs := append(edgeExtra, "--headless=new", "--disable-gpu", "--user-data-dir="+profile,
		"--window-size=900,300", "--screenshot="+shot, "https://"+site+"/")
	err = exec.Command(edge, edgeArgs...).Run()
	if err == nil {
		_, err = os.Stat(shot)
	}
	report("headless Edge screenshot", err, shot)

	if !ok {
		return fmt.Errorf("verification failed")
	}
	return nil
}
