package host

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/core/supervisor"
	"github.com/ebur-dev/ebur/internal/proc"
)

// mysqlSpec runs mysqld next to MariaDB (SVC-01) on its own port, setting
// up its data folder first if needed. It returns the port it uses.
func mysqlSpec(root paths.Root, version string) (supervisor.Spec, int, error) {
	dir := root.RuntimeDir("mysql", version)
	dataDir, _ := dbserver.Paths(root, dbserver.MySQL, version)
	if _, err := os.Stat(filepath.Join(dataDir, "mysql")); err != nil {
		// First start (or the data folder was removed): port, password,
		// trim and initialise.
		if _, err := provision.Prepare(context.Background(), root, "mysql", version, nil); err != nil {
			return supervisor.Spec{}, 0, err
		}
	} else if freed, err := provision.TrimMySQL(dir); err != nil {
		log.Printf("mysql: could not remove debug files: %v", err)
	} else if freed > 0 {
		log.Printf("mysql: removed %d MB of debug and developer files", freed>>20)
	}
	cfg, err := config.Load(root.Config())
	if err != nil {
		return supervisor.Spec{}, 0, err
	}
	if cfg.MySQLPort == 0 { // installed before a port was assigned
		cfg.MySQLPort = provision.PickMySQLPort(provision.MariaDBPortInUse(root, cfg))
		if err := config.Save(root.Config(), cfg); err != nil {
			return supervisor.Spec{}, 0, err
		}
	}
	ini, err := provision.WriteMySQLIni(root, version, cfg.MySQLPort)
	if err != nil {
		return supervisor.Spec{}, 0, err
	}
	cnf, err := provision.WriteClientCnf(root, dbserver.MySQL, version, cfg.MySQLPort, cfg.DBRootPassword)
	if err != nil {
		return supervisor.Spec{}, 0, err
	}
	admin := filepath.Join(dir, "bin", proc.Exe("mysqladmin"))
	return supervisor.Spec{
		Name:    "mysql",
		Path:    filepath.Join(dir, "bin", proc.Exe("mysqld")),
		Args:    []string{"--defaults-file=" + filepath.ToSlash(ini), "--console"}, // defaults-file must be first
		Dir:     dir,
		LogFile: filepath.Join(root.Logs(), "mysql.log"),
		Stop: func(ctx context.Context, _ int) error {
			// A clean shutdown avoids InnoDB crash recovery on next start.
			cmd := exec.CommandContext(ctx, admin, "--defaults-extra-file="+filepath.ToSlash(cnf), "shutdown")
			proc.Hide(cmd)
			return cmd.Run()
		},
	}, cfg.MySQLPort, nil
}
