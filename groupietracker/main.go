package main

import (
	"fmt"
	"log"
	"net/http"

	"groupietracker/router"
)

func main() {
	r := router.SetupRoutes()

	fmt.Println("http://localhost:8080")

	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
