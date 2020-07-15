package main

import (
	"os/signal"
	"sync"

	"os"
	"syscall"
)

func main() {
	// Open the database
	db := &macdatabase{}
	db.Open("./data.db")
	defer db.Close()

	// WaitGroup to track when our routines finish
	var wait sync.WaitGroup

	// Initialize the RADIUS server handler
	radius := NewRadiusServer(db)

	// Run the RADIUS server
	wait.Add(1)
	radius.Start(&wait)

	// Handle Ctrl-C
	ctrlc := make(chan os.Signal)
	signal.Notify(ctrlc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctrlc
		// Print a blank line to the console so the ^C doesn't mess up the output
		println("")
		radius.Stop()
	}()

	// Wait for the goroutines to finish
	wait.Wait()
}
