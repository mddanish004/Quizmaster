// Command migrate applies database migrations using the direct migrations URL.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("migrate: use `make migrate-up` (golang-migrate) or the migration SQL in database/migrations")
	return nil
}
