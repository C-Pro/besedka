package main

import (
	"besedka/internal/auth"
	"fmt"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: totp <secret>")
		os.Exit(1)
	}

	secret := os.Args[1]
	code, err := auth.GenerateTOTP(secret, time.Now())
	if err != nil {
		fmt.Printf("Error generating TOTP: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%06d\n", code)
}
