package main

import (
	"errors"
	"log"
	"os"
	"sync"

	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type macaddress struct {
	id         int64
	macaddress string
}

type devicegroup struct {
	id   int64
	name string
}

type ssid struct {
	id   int64
	ssid string
}

type macrecord struct {
	macaddress  macaddress
	devicegroup []devicegroup
	ssid        []ssid
}

// MACDatabase is a thing
type macdatabase struct {
	db        *sql.DB
	connected bool
	mux       sync.Mutex
}

// Open the MACDatabase by opening the database file
func (mdb *macdatabase) Open(filename string) error {
	mdb.mux.Lock()
	defer mdb.mux.Unlock()

	var err error
	var runInit bool

	// Check if the database file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		runInit = true
	}

	// Create or open the database
	if mdb.db, err = sql.Open("sqlite3", filename); err != nil {
		return errors.New("Unable to open database")
	}
	mdb.connected = true

	// Enable foreign key support
	if _, err = mdb.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return err
	}

	// If we need to initialize the database, do so now
	if runInit {
		if err = mdb.initialize(); err != nil {
			return err
		}
	}

	return nil
}

// Close the MACDatabase
func (mdb *macdatabase) Close() {
	mdb.mux.Lock()
	defer mdb.mux.Unlock()

	if mdb.connected {
		mdb.db.Close()
		mdb.connected = false
	}
}

// Initialize creates the table structure of the database
func (mdb *macdatabase) initialize() error {
	log.Println("DB: Initializing new database")

	if mdb.connected {
		sqlStmts := []string{
			`CREATE TABLE macaddress (
				id INTEGER NOT NULL PRIMARY KEY,
				macaddress TEXT NOT NULL UNIQUE
			);`,
			`CREATE INDEX macaddress_index ON macaddress(macaddress);`,
			`CREATE TABLE devicegroup (
				id INTEGER NOT NULL PRIMARY KEY,
				name TEXT NOT NULL UNIQUE
			);`,
			`CREATE TABLE ssid (
				id INTEGER NOT NULL PRIMARY KEY,
				ssid TEXT NOT NULL UNIQUE
			)`,
			`CREATE TABLE macaddress_devicegroup (
				macaddress INTEGER REFERENCES macaddress,
				devicegroup INTEGER REFERENCES devicegroup
			);`,
			`CREATE INDEX md_macaddress_index ON macaddress_devicegroup(macaddress);`,
			`CREATE INDEX md_devicegroup_index ON macaddress_devicegroup(devicegroup);`,
			`CREATE TABLE group_ssid (
				devicegroup INTEGER REFERENCES devicegroup,
				ssid INTEGER REFERENCES ssid
			);`,
			`CREATE INDEX gs_devicegroup_index ON group_ssid(devicegroup);`,
			`CREATE INDEX gs_ssid_index ON group_ssid(ssid);`,
		}

		for _, sqlStmt := range sqlStmts {
			if _, err := mdb.db.Exec(sqlStmt); err != nil {
				log.Println("DATABASE: Error initializing database: ", err, sqlStmt)
				return err
			}
		}
	}

	return nil
}

func (mdb *macdatabase) AddMACAddress(macaddress string) error {
	mdb.mux.Lock()
	defer mdb.mux.Unlock()

	stmt, err := mdb.db.Prepare("INSERT INTO macaddresses (macaddress) VALUES (?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(macaddress)
	if err != nil {
		return err
	}

	return nil
}

func (mdb *macdatabase) RemoveMACAddress(macaddress string) error {
	mdb.mux.Lock()
	defer mdb.mux.Unlock()

	stmt, err := mdb.db.Prepare("DELETE FROM macaddresses WHERE macaddress = ? LIMIT 1")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(macaddress)
	if err != nil {
		return err
	}

	return nil
}

func (mdb *macdatabase) GetMACAddresses() ([]macaddress, error) {
	mdb.mux.Lock()
	defer mdb.mux.Unlock()

	rows, err := mdb.db.Query("SELECT id, macaddress FROM macaddress")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var macs []macaddress
	var mac macaddress
	for rows.Next() {
		err = rows.Scan(&mac.id, &mac.macaddress)
		macs = append(macs, mac)
	}

	return macs, nil
}

func (mdb *macdatabase) GetMACRecord(m string) (*macrecord, error) {
	mdb.mux.Lock()
	defer mdb.mux.Unlock()

	var record macrecord

	// Get the macaddress record
	stmt, err := mdb.db.Prepare("SELECT id, macaddress FROM macaddress WHERE macaddress = ? LIMIT 1")
	if err != nil {
		return nil, err
	}
	if err = stmt.QueryRow(m).Scan(&record.macaddress.id, &record.macaddress.macaddress); err != nil {
		return nil, err
	}
	stmt.Close()

	var rows *sql.Rows

	// Get the groups
	if stmt, err = mdb.db.Prepare("SELECT DISTINCT dg.id, dg.name FROM devicegroup dg INNER JOIN macaddress_devicegroup md ON dg.id = md.devicegroup WHERE md.macaddress = ?"); err != nil {
		return nil, err
	}
	if rows, err = stmt.Query(record.macaddress.id); err != nil {
		return nil, err
	}
	var group devicegroup
	for rows.Next() {
		err = rows.Scan(&group.id, &group.name)
		record.devicegroup = append(record.devicegroup, group)
	}
	stmt.Close()

	// Get the SSIDs of the groups
	if stmt, err = mdb.db.Prepare("SELECT DISTINCT ss.id, ss.ssid FROM ssid ss INNER JOIN group_ssid gs ON ss.id = gs.ssid INNER JOIN macaddress_devicegroup md ON gs.devicegroup = md.devicegroup WHERE md.macaddress = ?"); err != nil {
		return nil, err
	}
	if rows, err = stmt.Query(record.macaddress.id); err != nil {
		return nil, err
	}
	var ssid ssid
	for rows.Next() {
		err = rows.Scan(&ssid.id, &ssid.ssid)
		record.ssid = append(record.ssid, ssid)
	}
	stmt.Close()

	return &record, nil
}
