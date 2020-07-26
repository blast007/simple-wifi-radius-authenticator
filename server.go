package main

import (
	"os/signal"
	"sync"

	"os"
	"syscall"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

func main() {
	// Open the database
	db, err := gorm.Open("sqlite3", "data.db")
	if err != nil {
		panic("Unable to create or open database")
	}
	defer db.Close()

	// Migrate the schema
	db.AutoMigrate(&Device{}, &DeviceGroup{}, &Network{}, &Client{})
	// WaitGroup to track when our routines finish
	var wait sync.WaitGroup

	// Initialize the RADIUS server handler
	radius := NewRadiusServer(db)

	// Run the RADIUS server
	wait.Add(1)
	radius.Start(&wait)

	// Initialize the WebUI
	webui := NewWebUI(db)

	// Run the WebUI server
	wait.Add(1)
	webui.Start(&wait)

	// Handle Ctrl-C
	ctrlc := make(chan os.Signal)
	signal.Notify(ctrlc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctrlc
		// Print a blank line to the console so the ^C doesn't mess up the output
		println("")
		webui.Stop()
		radius.Stop()
	}()

	// Wait for the goroutines to finish
	wait.Wait()
}
