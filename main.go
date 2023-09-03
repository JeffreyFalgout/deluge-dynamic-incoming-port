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
		start := time.Now()
		err := func() error {
			log.Printf("Attempting with %v timeout", timeout)
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			c := natpmp.NewClientWithTimeout(gatewayIP, timeout)
			addr, err := c.GetExternalAddress()
			if err != nil {
				return fmt.Errorf("could not get external address: %w", err)
			}
			log.Printf("External address: %v", net.IP(addr.ExternalIPAddress[:]))

			newMapping, err := c.AddPortMapping("tcp", 0, 0, 360)
			if err != nil {
				return fmt.Errorf("could not add port mapping: %w", err)
			}
			log.Printf("Added port mapping %d -> %d for %v", newMapping.InternalPort, newMapping.MappedExternalPort, time.Duration(newMapping.PortMappingLifetimeInSeconds)*time.Second)

			if mapping == nil || mapping.MappedExternalPort != newMapping.MappedExternalPort {
				if err := notifyDeluge(ctx, newMapping.MappedExternalPort); err != nil {
					return fmt.Errorf("could not notify deluge about the port mapping: %w", err)
				}
				mapping = newMapping
			}

			timeout = max(timeout/2, 250*time.Millisecond)
			time.Sleep(time.Duration(mapping.PortMappingLifetimeInSeconds) * time.Second / 2)
			return nil
		}()
		if err != nil {
			timeout *= 2
			log.Print(err)
			dur := time.Since(start)
			sleep := timeout - dur
			time.Sleep(sleep)
		}
	}
}

func notifyDeluge(ctx context.Context, port uint16) error {
	return fmt.Errorf("TODO")
}
