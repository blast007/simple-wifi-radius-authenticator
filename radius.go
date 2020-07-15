package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// RadiusServer runs the RADIUS server
type RadiusServer struct {
	Addr string
	Port uint16
	DB   *macdatabase

	server *radius.PacketServer
}

// NewRadiusServer creates a new instance of RadiusServer
func NewRadiusServer(db *macdatabase) RadiusServer {
	radiusserver := RadiusServer{}
	radiusserver.Port = 1812
	radiusserver.DB = db
	return radiusserver
}

// Start the RADIUS server
func (rs *RadiusServer) Start(wait *sync.WaitGroup) {
	// Initialize the RADIUS server handler
	rs.server = &radius.PacketServer{
		Handler:      radius.HandlerFunc(rs.radiusHandler),
		SecretSource: radius.StaticSecretSource([]byte(`secret`)),
		Addr:         fmt.Sprintf("%v:%v", rs.Addr, rs.Port),
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
	stripDelimiters := strings.NewReplacer(":", "", "-", "", ".", "")

	// Default to rejecting the request
	code := radius.CodeAccessReject

	// Convert username lowercase and remove delimiters
	mac := stripDelimiters.Replace(strings.ToLower(username))

	// Verify the value looks like a MAC address
	validFormat, _ := regexp.MatchString(`^[0-9a-f]{12}$`, mac)

	// Parse the SSID out of the Called-Station-Id
	csiParts := strings.Split(calledStationID, ":")
	requestedSSID := csiParts[len(csiParts)-1]

	switch {
	case nasPortType != rfc2865.NASPortType_Value_Wireless80211 && nasPortType != rfc2865.NASPortType_Value_WirelessOther:
		log.Println("RADIUS: Invalid NAS-Port-Type (must be wireless)")
	case !validFormat:
		log.Println("RADIUS: Invalid MAC address format received")
	default:
		if record, err := rs.DB.GetMACRecord(mac); err != nil {
			// TODO: Pull allowed SSIDs for NULL group id
			log.Println("Did not find record of", mac, err)
		} else {

			// Verify the SSID is allowed
			for _, ssid := range record.ssid {
				if ssid.ssid == requestedSSID {
					code = radius.CodeAccessAccept
				}
			}
		}
		log.Printf("RADIUS: %v received %v for %v", mac, code, requestedSSID)
	}

	w.Write(r.Response(code))
}
