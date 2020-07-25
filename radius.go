package main

import (
	"context"
	"log"
	"strings"
	"sync"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

// RadiusServer runs the RADIUS server
type RadiusServer struct {
	Addr string
	DB   *gorm.DB

	server *radius.PacketServer
}

// NewRadiusServer creates a new instance of RadiusServer
func NewRadiusServer(db *gorm.DB) RadiusServer {
	radiusserver := RadiusServer{}
	radiusserver.Addr = ":1812"
	radiusserver.DB = db
	return radiusserver
}

// Start the RADIUS server
func (rs *RadiusServer) Start(wait *sync.WaitGroup) {
	// Initialize the RADIUS server handler
	rs.server = &radius.PacketServer{
		Handler:      radius.HandlerFunc(rs.radiusHandler),
		SecretSource: radius.StaticSecretSource([]byte(`secret`)),
		Addr:         rs.Addr,
	}

	go func(rs *RadiusServer, wait *sync.WaitGroup) {
		log.Printf("RADIUS: Starting server on %v", rs.server.Addr)

		if err := rs.server.ListenAndServe(); err != nil && err != radius.ErrServerShutdown {
			log.Printf("WEBUI: Error starting RADIUS server: %v", err)
		} else {
			log.Printf("RADIUS: Stopped server")
		}

		wait.Done()
	}(rs, wait)
}

// Stop the RADIUS server
func (rs *RadiusServer) Stop() {
	rs.server.Shutdown(context.Background())
}

func (rs *RadiusServer) radiusHandler(w radius.ResponseWriter, r *radius.Request) {
	username := rfc2865.UserName_GetString(r.Packet)
	nasPortType := rfc2865.NASPortType_Get(r.Packet)
	calledStationID := rfc2865.CalledStationID_GetString(r.Packet)
	// TODO: Use the password for something. Some WiFi controllers will pass the MAC address again while others may use a shared password for all devices.
	//password := rfc2865.UserPassword_GetString(r.Packet)

	// Default to rejecting the request
	code := radius.CodeAccessReject

	// Convert username lowercase and remove delimiters
	mac := normalizeMACAddress(username)

	// Parse the SSID out of the Called-Station-Id
	csiParts := strings.Split(calledStationID, ":")
	requestedSSID := csiParts[len(csiParts)-1]

	switch {
	// Must be a wireless port type
	case nasPortType != rfc2865.NASPortType_Value_Wireless80211 && nasPortType != rfc2865.NASPortType_Value_WirelessOther:
		log.Println("RADIUS: Invalid NAS-Port-Type (must be wireless)")
	// Verify the value looks like a MAC address
	case !isValidMACFormat(mac):
		log.Println("RADIUS: Invalid MAC address format received")
	// Look up the record
	default:
		var device Device
		rs.DB.Preload("DeviceGroups").Preload("DeviceGroups.Networks").First(&device, "MAC = ?", mac)
		if device.ID > 0 {
			// Verify the requested SSID is allowed
			for _, group := range device.DeviceGroups {
				for _, network := range group.Networks {
					if network.SSID == requestedSSID {
						code = radius.CodeAccessAccept
					}
				}
			}
			log.Println("RADIUS: Found:", device.MAC)
		} else {
			// TODO: Pull allowed SSIDs for NULL group id
			log.Println("RADIUS: Not found:", mac)
		}

		log.Printf("RADIUS: %v received %v for %v", mac, code, requestedSSID)
	}

	w.Write(r.Response(code))
}
