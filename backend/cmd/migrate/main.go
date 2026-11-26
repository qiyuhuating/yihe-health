// Command migrate runs the MySQL schema migrations and exits.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"yihe-care/internal/db"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, os.Getenv("MYSQL_DSN"))
	if err != nil {
		log.Fatal("MySQL connection failed: check MYSQL_DSN")
	}
	defer pool.Close()
	if err := db.MigrateMySQL(ctx, pool); err != nil {
		log.Fatal(err)
	}
	log.Println("MySQL migrations applied")
}
