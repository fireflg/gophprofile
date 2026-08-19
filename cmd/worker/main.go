// Command worker обрабатывает события аватарок: нарезает миниатюры и чистит хранилище.
package main

import (
	"os"

	"github.com/fireflg/gophprofile/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		logger.FatalStartup("worker stopped with error", err)
		os.Exit(1)
	}
}
