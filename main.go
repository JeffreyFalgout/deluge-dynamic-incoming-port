package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	natpmp "github.com/jackpal/go-nat-pmp"
)

func main() {
	ctx := context.Background()

	gateway, ok := os.LookupEnv("NATPMP_GATEWAY")
	if !ok {
		log.Fatalf("You must specify NATPMP_GATEWAY as an environment variable.")
	}
	gatewayIP := net.ParseIP(gateway)
	if gatewayIP == nil {
		log.Fatalf("NATPMP_GATEWAY was invalid: %q", gateway)
	}

	var mapping *natpmp.AddPortMappingResult
	timeout := 250 * time.Millisecond
	for {
		func() {
			log.Printf("Attempting with %v timeout", timeout)
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			c := natpmp.NewClientWithTimeout(gatewayIP, timeout)
			addr, err := c.GetExternalAddress()
			if err != nil {
				log.Printf("Could not get external address: %v", err)
				timeout *= 2
				return
			}
			log.Printf("External address: %v", net.IP(addr.ExternalIPAddress[:]))

			newMapping, err := c.AddPortMapping("tcp", 0, 0, 360)
			if err != nil {
				log.Printf("Could not add port mapping: %v", err)
				timeout *= 2
				return
			}
			log.Printf("Added port mapping %d -> %d for %v", newMapping.InternalPort, newMapping.MappedExternalPort, time.Duration(newMapping.PortMappingLifetimeInSeconds)*time.Second)

			if mapping == nil || mapping.MappedExternalPort != newMapping.MappedExternalPort {
				if err := notifyDeluge(ctx, newMapping.MappedExternalPort); err != nil {
					log.Printf("Could not notify deluge about the port mapping: %v", err)
					timeout *= 2
					return
				}
				mapping = newMapping
			}

			timeout = max(timeout/2, 250*time.Millisecond)
			time.Sleep(time.Duration(mapping.PortMappingLifetimeInSeconds) * time.Second / 2)
		}()
	}
}

func notifyDeluge(ctx context.Context, port uint16) error {
	return fmt.Errorf("TODO")
}
