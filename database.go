package main

import (
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

// Device stores the MAC addresses and is associated with zero or more device groups
type Device struct {
	gorm.Model
	MAC          string        `gorm:"unique;not null"`
	DeviceGroups []DeviceGroup `gorm:"many2many:device_devicegroups;"`
}

// DeviceGroup store the groups a device can belong to and is associated with zero or more networks
type DeviceGroup struct {
	gorm.Model
	Name     string    `gorm:"unique;not null"`
	Networks []Network `gorm:"many2many:devicegroup_ssids;"`
}

// Network store the known SSIDs
type Network struct {
	gorm.Model
	SSID string `gorm:"unique;not null"`
}

// Client stores settings about each RADIUS client
type Client struct {
	gorm.Model
	ClientIP     string `gorm:"unique;not null"`
	PasswordMode int
	Secret       string
}

// ClientPasswordMode defines how we process the password supplied by a RADIUS client
type ClientPasswordMode int

const (
	// ClientPasswordModeIgnore will ignore the provided password
	ClientPasswordModeIgnore ClientPasswordMode = 0
	// ClientPasswordModeMAC will treat the password as a MAC address and compare it to the username
	ClientPasswordModeMAC = 1
	// ClientPasswordModeSharedSecret will treat the password as a secondary shared secret that the RADIUS client will provide
	ClientPasswordModeSharedSecret = 2
)
