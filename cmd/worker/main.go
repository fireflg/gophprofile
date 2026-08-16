// Command worker обрабатывает события аватарок: нарезает миниатюры и чистит хранилище.
package main

import (
	"log"
	"os"
)

func main() {
	if err := run(); err != nil {
		log.Printf("worker stopped with error: %v", err)
		os.Exit(1)
	}
}
