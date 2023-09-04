package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/egymgmbh/go-prefix-writer/prefixer"
	natpmp "github.com/jackpal/go-nat-pmp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golift.io/deluge"
)

var verbose = flag.Bool("verbose", false, "Whether to log verbosely.")

func main() {
	flag.Parse()

	ctx := context.Background()

	l := zerolog.InfoLevel
	if *verbose {
		l = zerolog.TraceLevel
	}
	out := zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = prefixer.New(os.Stderr, func() string { return "[deluge-dynamic-incoming-port] " })
		w.TimeFormat = time.RFC3339
	})
	log.Logger = zerolog.New(out).With().Timestamp().Logger().Level(l)
	zerolog.DefaultContextLogger = &log.Logger
	zerolog.SetGlobalLevel(l)

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

	deluge, err := newDelugeClient("http://localhost:8112")
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Could not create Deluge client")
	}
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

func run(ctx context.Context, gateway net.IP, d *delugeWrapper) (time.Duration, error) {
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

type delugeWrapper struct {
	deluge *deluge.Deluge
	host   string
	port   uint16

	noopLogSampler zerolog.Sampler
}

func newDelugeClient(addr string) (*delugeWrapper, error) {
	deluge, err := deluge.NewNoAuth(&deluge.Config{
		URL:      addr,
		Password: "deluge",
		Client: &http.Client{
			Transport: loggingRoundTripper{http.DefaultTransport},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("deluge.NewNoAuth() = %w", err)
	}
	return &delugeWrapper{deluge: deluge, noopLogSampler: &timeSampler{Every: 30 * time.Minute}}, nil
}

func (d *delugeWrapper) updateIncomingPort(ctx context.Context, port uint16) error {
	log := log.Ctx(ctx)
	if port == d.port {
		l := zerolog.DebugLevel
		if d.noopLogSampler.Sample(zerolog.InfoLevel) {
			l = zerolog.InfoLevel
		}
		log.WithLevel(l).
			Uint16("port", port).
			Msg("Not updating Deluge since we think its port mapping is already correct.")
		return nil
	}

	if d.host == "" {
		hostsResp, err := d.deluge.Get(ctx, deluge.GeHosts, []string{})
		if err != nil {
			return fmt.Errorf("could not list Deluge hosts: %w", err)
		}

		var servers [][]any
		if err := json.Unmarshal(hostsResp.Result, &servers); err != nil {
			return fmt.Errorf("could not unmarshal hosts list: %w", err)
		}

		var arr zerolog.Array
		for _, s := range servers {
			arr.Interface(s)
		}
		log.Debug().
			Array("hosts", &arr).
			Msg("Discovered deluge hosts")

		host, ok := servers[0][0].(string)
		if !ok {
			return fmt.Errorf("deluge hosts list has unexpected shape: %#v", servers)
		}

		d.host = host
		if _, err := d.deluge.Get(ctx, "web.connect", []string{host}); err != nil {
			return fmt.Errorf("could not connect to Deluge: %w", err)
		}
		log.Debug().
			Str("host", host).
			Msg("Connected to Deluge")
	}

	if _, err := d.deluge.Get(ctx, "core.set_config", []map[string]any{map[string]any{"listen_ports": []uint16{port, port}}}); err != nil {
		return fmt.Errorf("could not update Deluge incoming port: %w", err)
	}
	log.Info().
		Uint16("port", port).
		Msg("Updated Delgue incoming port.")
	d.port = port
	return nil
}

type loggingRoundTripper struct {
	delegate http.RoundTripper
}

func (rt loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	log := log.Ctx(req.Context())
	if err := logRequest(log, req); err != nil {
		return nil, err
	}
	resp, err := rt.delegate.RoundTrip(req)
	if resp != nil {
		if err := logResponse(log, resp); err != nil {
			return nil, err
		}
	}
	return resp, err
}

func logRequest(log *zerolog.Logger, req *http.Request) error {
	hdrs := zerolog.Dict()
	for k, v := range req.Header {
		hdrs.Strs(k, v)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))

	log.Debug().
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Dict("headers", hdrs).
		Str("body", string(body)).
		Msg("Sending HTTP request to Deluge.")
	return nil
}

func logResponse(log *zerolog.Logger, resp *http.Response) error {
	hdrs := zerolog.Dict()
	for k, v := range resp.Header {
		hdrs.Strs(k, v)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	log.Debug().
		Str("status", resp.Status).
		Dict("headers", hdrs).
		Str("body", string(body)).
		Msg("Received response from Deluge.")
	return nil
}

type timeSampler struct {
	Every time.Duration
	last  time.Time
}

func (s *timeSampler) Sample(l zerolog.Level) bool {
	now := time.Now()
	if now.Sub(s.last) >= s.Every {
		s.last = now
		return true
	}
	return false
}
