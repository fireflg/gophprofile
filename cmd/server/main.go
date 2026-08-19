// Command server поднимает HTTP-API и веб-интерфейс сервиса аватарок.
package main

import (
	"log"
	"os"
)

func main() {
	if err := run(); err != nil {
		log.Printf("server stopped with error: %v", err)
		os.Exit(1)
	}
}
