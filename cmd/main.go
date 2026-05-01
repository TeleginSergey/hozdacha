package main

import (
	"log"

	"github.com/TeleginSergey/hozdacha/internal/app"
)

func main() {
	appInstance, err := app.NewApp()
	if err != nil {
		log.Fatalf("failed to initialize app: %v", err)
	}
	appInstance.Run()
}
