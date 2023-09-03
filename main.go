package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	natpmp "github.com/jackpal/go-nat-pmp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	ctx := context.Background()

	out := zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stderr
		w.TimeFormat = time.RFC3339
	})
	log.Logger = zerolog.New(out).With().Timestamp().Logger()
	zerolog.DefaultContextLogger = &log.Logger

	gateway, ok := os.LookupEnv("NATPMP_GATEWAY")
	if !ok {
		log.Fatal().
			Msg("You must specify NATPMP_GATEWAY as an environment variable.")
	}
	gatewayIP := net.ParseIP(gateway)
	if gatewayIP == nil {
		log.Fatal().
			Str("NATPMP_GATEWAY", gateway).
			Msg("NATPMP_GATEWAY is invalid")
	}

	deluge := newDelugeClient("localhost:8117")
	minTimeout := 250 * time.Millisecond
	maxTimeout := 5 * time.Minute
	timeout := minTimeout
	for {
		func() {
			log.Debug().
				Dur("timeout", timeout).
				Msg("Attempting to map a port...")
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			ttl, err := run(ctx, gatewayIP, deluge)
			if err != nil {
				timeout = min(timeout*2, maxTimeout)
				log.Error().Err(err).Send()
				dl, _ := ctx.Deadline()
				time.Sleep(time.Until(dl))
				return
			}

			timeout = max(timeout/2, minTimeout)
			time.Sleep(ttl / 2)
		}()
	}
}

func run(ctx context.Context, gateway net.IP, d *deluge) (time.Duration, error) {
	p, ttl, err := requestPortMapping(ctx, gateway)
	if err != nil {
		return 0, err
	}
	if err := d.updateIncomingPort(ctx, p); err != nil {
		return 0, err
	}
	return ttl, nil
}

func requestPortMapping(ctx context.Context, gateway net.IP) (uint16, time.Duration, error) {
	log := log.Ctx(ctx)

	var c *natpmp.Client
	dl, ok := ctx.Deadline()
	if ok {
		c = natpmp.NewClientWithTimeout(gateway, time.Until(dl))
	} else {
		c = natpmp.NewClient(gateway)
	}
	addr, err := c.GetExternalAddress()
	if err != nil {
		return 0, 0, fmt.Errorf("could not get external address: %w", err)
	}
	log.Debug().
		IPAddr("external_address", net.IP(addr.ExternalIPAddress[:])).
		Send()

	m, err := c.AddPortMapping("tcp", 0, 0, 360)
	if err != nil {
		return 0, 0, fmt.Errorf("could not add port mapping: %w", err)
	}

	ttl := time.Duration(m.PortMappingLifetimeInSeconds) * time.Second
	log.Debug().
		Uint16("internal_port", m.InternalPort).
		Uint16("external_port", m.MappedExternalPort).
		Dur("ttl", ttl).
		Msg("Added port mapping")
	return m.MappedExternalPort, ttl, nil
}

type deluge struct {
	addr string
	port uint16
}

func newDelugeClient(addr string) *deluge {
	return &deluge{addr: addr}
}

func (d *deluge) updateIncomingPort(ctx context.Context, port uint16) error {
	log := log.Ctx(ctx)
	if port == d.port {
		log.Debug().
			Uint16("port", port).
			Msg("Not updating Deluge since we think its port mapping is already correct.")
		return nil
	}

	return fmt.Errorf("todo")
}
