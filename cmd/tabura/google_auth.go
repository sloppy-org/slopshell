package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/googleauth"
)

func cmdGoogleAuth() error {
	session, err := googleauth.New("", "", googleauth.DefaultScopes)
	if err != nil {
		return err
	}
	authURL := session.GetAuthURL()
	fmt.Println("Open this URL in your browser:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	fmt.Println("Waiting for callback (or paste the authorization code below)...")

	tokenPath := session.TokenPath()
	var modBefore time.Time
	if info, err := os.Stat(tokenPath); err == nil {
		modBefore = info.ModTime()
	}

	codeCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			codeCh <- strings.TrimSpace(scanner.Text())
		}
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(5 * time.Minute)
	for {
		select {
		case code := <-codeCh:
			if code == "" {
				return fmt.Errorf("empty authorization code")
			}
			if err := session.ExchangeCode(context.Background(), code); err != nil {
				return err
			}
			fmt.Printf("Token saved to %s\n", tokenPath)
			return nil
		case <-ticker.C:
			if info, err := os.Stat(tokenPath); err == nil && info.ModTime().After(modBefore) {
				fmt.Printf("Token updated via callback at %s\n", tokenPath)
				return nil
			}
		case <-timeout:
			return fmt.Errorf("timed out waiting for authorization")
		}
	}
}
