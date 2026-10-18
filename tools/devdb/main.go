// devdb runs an isolated loopback-only PostgreSQL for local development and tests.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func main() {
	rootFlag := flag.String("root", "../../.runtime/postgres", "Task-owned database directory")
	port := flag.Uint("port", 55432, "Loopback PostgreSQL port")
	flag.Parse()
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		log.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(root), "/.runtime/postgres") {
		log.Fatal("devdb root must end in the task-owned /.runtime/postgres directory")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		log.Fatal(err)
	}
	password := os.Getenv("DEV_DATABASE_PASSWORD")
	if password == "" {
		password = "studio-local-development"
	}
	db := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().Port(uint32(*port)).Username("studio").Password(password).Database("postgres").RuntimePath(filepath.Join(root, "runtime")).DataPath(filepath.Join(root, "data")).BinariesPath(filepath.Join(root, "binaries")).CachePath(filepath.Join(root, "cache")).Locale("C").Encoding("UTF8").StartTimeout(90 * time.Second))
	if err = db.Start(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Development PostgreSQL ready on 127.0.0.1:%d; data: %s\n", *port, filepath.Join(root, "data"))
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	if err = db.Stop(); err != nil {
		log.Print(err)
	}
}
