// Spike: run several PHP versions at once on Windows behind Nginx,
// using a supervised pool of php-cgi.exe processes per version.
//
// Questions this spike answers (SRS §8, PHP-04):
//   - Can two PHP versions serve different sites at the same time?
//   - Does a slow request block others when pool size > 1?
//   - Does the supervisor respawn php-cgi after PHP_FCGI_MAX_REQUESTS?
//   - Does a Job Object kill every child if the supervisor dies?
//   - Do paths with spaces work in nginx config?
//
// Usage: phppool -test   (start, run self-tests, stop)
//
//	phppool         (start and run until Ctrl+C)
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type pool struct {
	name     string // upstream name, e.g. php83
	host     string // site host, e.g. v83.test
	phpDir   string
	basePort int
	size     int
}

const (
	httpPort    = 8088
	maxRequests = 50 // low on purpose so the respawn path gets exercised
)

var (
	spikeDir string
	runtimes string
	workDir  string
	nginxDir string
	stopping atomic.Bool
	respawns atomic.Int64
	wg       sync.WaitGroup
	procsMu  sync.Mutex
	procs    = map[int]*os.Process{}
)

func main() {
	test := flag.Bool("test", false, "run self-tests then exit")
	flag.Parse()

	exe, _ := os.Executable()
	spikeDir = filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(spikeDir, "main.go")); err != nil {
		spikeDir, _ = os.Getwd() // `go run` builds into a temp dir
	}
	runtimes = filepath.Join(spikeDir, "..", "runtimes")
	workDir = filepath.Join(spikeDir, "work")
	nginxDir = filepath.Join(runtimes, "nginx", "nginx-1.30.8")

	must(joinKillOnCloseJob())

	pools := []pool{
		{"php83", "v83.test", filepath.Join(runtimes, "php83"), 9830, 4},
		{"php84", "v84.test", filepath.Join(runtimes, "php84"), 9840, 4},
	}

	t0 := time.Now()
	must(writeSites(pools))
	for _, p := range pools {
		for i := 0; i < p.size; i++ {
			wg.Add(1)
			// Stagger request limits (50, 62, 75, 87) so workers never all expire together.
			go supervise(p, p.basePort+i, maxRequests+i*maxRequests/4)
		}
	}
	must(writeNginxConf(pools))
	must(startNginx())
	must(waitReady(pools, 10*time.Second))
	log.Printf("all services ready in %v", time.Since(t0).Round(time.Millisecond))

	if *test {
		ok := runTests(pools)
		shutdown()
		if !ok {
			os.Exit(1)
		}
		return
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	log.Printf("serving on http://127.0.0.1:%d (pid %d); Ctrl+C to stop", httpPort, os.Getpid())
	<-c
	shutdown()
}

// joinKillOnCloseJob puts this process in a Job Object with
// KILL_ON_JOB_CLOSE. Children inherit the job, so if this process dies
// for any reason the OS closes the handle and kills every descendant.
func joinKillOnCloseJob() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return err
	}
	return windows.AssignProcessToJobObject(job, windows.CurrentProcess())
}

// supervise keeps one php-cgi process alive on one port.
func supervise(p pool, port, maxReq int) {
	defer wg.Done()
	for !stopping.Load() {
		cmd := exec.Command(filepath.Join(p.phpDir, "php-cgi.exe"), "-b", fmt.Sprintf("127.0.0.1:%d", port))
		cmd.Env = append(os.Environ(), fmt.Sprintf("PHP_FCGI_MAX_REQUESTS=%d", maxReq))
		cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			log.Printf("%s:%d start failed: %v", p.name, port, err)
			time.Sleep(time.Second)
			continue
		}
		procsMu.Lock()
		procs[cmd.Process.Pid] = cmd.Process
		procsMu.Unlock()
		err := cmd.Wait()
		procsMu.Lock()
		delete(procs, cmd.Process.Pid)
		procsMu.Unlock()
		if stopping.Load() {
			return
		}
		respawns.Add(1)
		log.Printf("%s:%d exited (%v), respawning", p.name, port, err)
	}
}

func fwd(s string) string { return filepath.ToSlash(s) }

func writeSites(pools []pool) error {
	for _, p := range pools {
		dir := filepath.Join(workDir, "www", p.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		files := map[string]string{
			"index.php": "<?php echo PHP_VERSION, ' pid=', getmypid();",
			"slow.php":  "<?php sleep(3); echo 'slow ', PHP_VERSION, ' pid=', getmypid();",
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeNginxConf(pools []pool) error {
	for _, d := range []string{"conf", "logs", "temp"} {
		if err := os.MkdirAll(filepath.Join(workDir, d), 0o755); err != nil {
			return err
		}
	}
	for _, f := range []string{"fastcgi_params", "mime.types"} {
		b, err := os.ReadFile(filepath.Join(nginxDir, "conf", f))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(workDir, "conf", f), b, 0o644); err != nil {
			return err
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "worker_processes 1;\nerror_log logs/error.log warn;\npid logs/nginx.pid;\n")
	fmt.Fprintf(&sb, "events { worker_connections 256; }\nhttp {\n  include mime.types;\n  access_log off;\n")
	fmt.Fprintf(&sb, "  client_body_temp_path temp/body; fastcgi_temp_path temp/fastcgi; proxy_temp_path temp/proxy;\n")
	fmt.Fprintf(&sb, "  uwsgi_temp_path temp/uwsgi; scgi_temp_path temp/scgi;\n")
	for _, p := range pools {
		// least_conn: php-cgi handles one request at a time, so prefer idle workers.
		fmt.Fprintf(&sb, "  upstream %s {\n    least_conn;\n", p.name)
		for i := 0; i < p.size; i++ {
			fmt.Fprintf(&sb, "    server 127.0.0.1:%d max_fails=0;\n", p.basePort+i)
		}
		fmt.Fprintf(&sb, "  }\n")
		fmt.Fprintf(&sb, "  server {\n    listen 127.0.0.1:%d;\n    server_name %s;\n", httpPort, p.host)
		fmt.Fprintf(&sb, "    root \"%s\";\n    index index.php;\n", fwd(filepath.Join(workDir, "www", p.name)))
		fmt.Fprintf(&sb, "    location ~ \\.php$ {\n      include fastcgi_params;\n")
		fmt.Fprintf(&sb, "      fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
		fmt.Fprintf(&sb, "      fastcgi_next_upstream error timeout;\n      fastcgi_pass %s;\n    }\n  }\n", p.name)
	}
	sb.WriteString("}\n")
	return os.WriteFile(filepath.Join(workDir, "conf", "nginx.conf"), []byte(sb.String()), 0o644)
}

func nginxCmd(args ...string) *exec.Cmd {
	all := append([]string{"-p", fwd(workDir) + "/", "-c", "conf/nginx.conf"}, args...)
	cmd := exec.Command(filepath.Join(nginxDir, "nginx.exe"), all...)
	cmd.Dir = workDir
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true}
	return cmd
}

func startNginx() error {
	if out, err := nginxCmd("-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx -t: %v\n%s", err, out)
	}
	return nginxCmd().Start() // nginx on Windows stays in the foreground; we don't wait on it
}

func get(host, path string) (string, time.Duration, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", httpPort, path), nil)
	req.Host = host
	t := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", time.Since(t), err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return string(b), time.Since(t), fmt.Errorf("status %d", resp.StatusCode)
	}
	return string(b), time.Since(t), nil
}

func waitReady(pools []pool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for _, p := range pools {
		for {
			if _, _, err := get(p.host, "/"); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s not ready after %v", p.host, timeout)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return nil
}

func runTests(pools []pool) bool {
	ok := true
	check := func(name string, pass bool, detail string) {
		mark := "PASS"
		if !pass {
			mark, ok = "FAIL", false
		}
		fmt.Printf("[%s] %s: %s\n", mark, name, detail)
	}

	// 1. Each site gets its own PHP version.
	for _, p := range pools {
		body, _, err := get(p.host, "/")
		check("version "+p.host, err == nil && strings.HasPrefix(body, p.name[3:4]+"."+p.name[4:]), body)
	}

	// 2. Three slow requests in flight must not block fast ones (pool of 4).
	var swg sync.WaitGroup
	for i := 0; i < 3; i++ {
		swg.Add(1)
		go func() { defer swg.Done(); get("v83.test", "/slow.php") }()
	}
	time.Sleep(300 * time.Millisecond)
	var worst time.Duration
	var fastErr error
	for i := 0; i < 10; i++ {
		_, d, err := get("v83.test", "/")
		if err != nil {
			fastErr = err
		}
		if d > worst {
			worst = d
		}
	}
	swg.Wait()
	check("fast requests while 3 slow in flight", fastErr == nil && worst < time.Second,
		fmt.Sprintf("worst latency %v, err=%v", worst.Round(time.Millisecond), fastErr))

	// 2b. Saturate the pool: 4 slow + 1 fast shows what happens when every worker is busy.
	for i := 0; i < 4; i++ {
		swg.Add(1)
		go func() { defer swg.Done(); get("v83.test", "/slow.php") }()
	}
	time.Sleep(300 * time.Millisecond)
	_, d, err := get("v83.test", "/")
	swg.Wait()
	fmt.Printf("[INFO] fast request with pool saturated: %v (err=%v); expected to wait for a free worker\n",
		d.Round(time.Millisecond), err)

	// 3. Respawn: 300 requests across a pool of 4 with max 50 requests each.
	before := respawns.Load()
	fails := 0
	var cwg sync.WaitGroup
	var mu sync.Mutex
	for w := 0; w < 4; w++ {
		cwg.Add(1)
		go func() {
			defer cwg.Done()
			for i := 0; i < 75; i++ {
				if _, _, err := get("v84.test", "/"); err != nil {
					mu.Lock()
					fails++
					mu.Unlock()
				}
			}
		}()
	}
	cwg.Wait()
	got := respawns.Load() - before
	check("respawn after max requests", got >= 3 && fails == 0,
		fmt.Sprintf("%d respawns, %d failed requests of 300", got, fails))
	return ok
}

func shutdown() {
	stopping.Store(true)
	if out, err := nginxCmd("-s", "quit").CombinedOutput(); err != nil {
		log.Printf("nginx -s quit: %v %s", err, out)
	}
	procsMu.Lock()
	for _, p := range procs {
		p.Kill() // php-cgi has no graceful stop on Windows
	}
	procsMu.Unlock()
	wg.Wait()
	log.Printf("stopped")
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
