package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	app "github.com/xuanranran/ztncui/console/internal/console"
	"github.com/xuanranran/ztncui/console/ui"
)

func main() {
	demo := flag.Bool("demo", false, "Use isolated local preview data")
	disableProbes := flag.Bool("disable-probes", false, "Disable managed private-IP ICMP reachability probes")
	addr := flag.String("addr", "127.0.0.1:3101", "HTTP listen address")
	db := flag.String("db", "data/console.sqlite", "SQLite management database")
	zt := flag.String("zt-address", "http://127.0.0.1:9993", "Local ZeroTier controller API")
	credential := "/var/lib/zerotier-one/authtoken.secret"
	if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		credential = filepath.Join(dir, "zerotier-token")
	}
	token := flag.String("zt-token-file", credential, "ZeroTier authentication token file")
	legacy := flag.String("legacy-dir", "", "Read-only import of the old etc directory")
	webDir := flag.String("web-dir", "", "Optional built frontend directory for local development")
	flag.Parse()
	if *demo {
		host, _, err := net.SplitHostPort(*addr)
		if err != nil || host != "localhost" && net.ParseIP(host) != nil && !net.ParseIP(host).IsLoopback() || host != "localhost" && net.ParseIP(host) == nil {
			log.Fatal("Preview must bind to a loopback address")
		}
	}
	files, err := fs.Sub(ui.Files, "dist")
	if err != nil {
		log.Fatal(err)
	}
	if *webDir != "" {
		files = os.DirFS(*webDir)
	}
	server, err := app.New(app.Config{Demo: *demo, DisableProbes: *disableProbes, Database: *db, Address: *zt, TokenFile: *token, Token: os.Getenv("ZT_TOKEN"), LegacyDir: *legacy}, files)
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = app.Serve(ctx, *addr, server); err != nil {
		log.Fatal(err)
	}
}
