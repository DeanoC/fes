package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brutella/dnssd"
	"github.com/hashicorp/mdns"
)

const (
	serviceType     = "_fogcast._tcp"
	serviceFQDN     = serviceType + ".local."
	protocolVersion = "1"
)

// lookupType is the DNS-SD browse used by Resolve. brutella/dnssd packet
// readers only stop on context.Canceled; a parent deadline would leave them
// spinning on closed UDP sockets after LookupType returns.
var lookupType = lookupTypeUntilParentDone
var readRandom = rand.Read

var (
	networkFingerprint        = usableNetworkFingerprint
	advertisementPollInterval = time.Second
	advertisementRetryInitial = time.Second
	advertisementRetryMaximum = 15 * time.Second
	advertisementWait         = waitContext
)

type advertiser interface {
	Respond(context.Context) error
}

type advertisementConfig struct {
	name string
	host string
	port int
	text []string
}

type mdnsAdvertiser struct{ servers []*mdns.Server }

func (a *mdnsAdvertiser) Respond(ctx context.Context) error {
	<-ctx.Done()
	var result error
	for _, server := range a.servers {
		result = errors.Join(result, server.Shutdown())
	}
	return errors.Join(ctx.Err(), result)
}

var newAdvertiser = func(cfg advertisementConfig) (advertiser, error) {
	interfaces := usableNetworkInterfaces()
	servers := make([]*mdns.Server, 0, len(interfaces))
	for _, iface := range interfaces {
		addresses, _ := iface.Addrs()
		ips := make([]net.IP, 0, len(addresses))
		for _, address := range addresses {
			if ip, _, parseErr := net.ParseCIDR(address.String()); parseErr == nil && !ip.IsLoopback() && !ip.IsUnspecified() {
				ips = append(ips, ip)
			}
		}
		service, serviceErr := mdns.NewMDNSService(cfg.name, serviceType, "local.", cfg.host, cfg.port, ips, cfg.text)
		if serviceErr != nil {
			shutdownServers(servers)
			return nil, serviceErr
		}
		server, serverErr := mdns.NewServer(&mdns.Config{Zone: service, Iface: &iface, Logger: log.New(io.Discard, "", 0)})
		if serverErr != nil {
			shutdownServers(servers)
			return nil, serverErr
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return nil, errors.New("no usable multicast interfaces")
	}
	return &mdnsAdvertiser{servers: servers}, nil
}

func ValidID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	for i := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((id[i] >= '0' && id[i] <= '9') || (id[i] >= 'a' && id[i] <= 'f')) {
			return false
		}
	}
	return true
}

func NewID() (string, error) {
	var bytes [16]byte
	if _, err := readRandom(bytes[:]); err != nil {
		return "", fmt.Errorf("generate target ID: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], bytes[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], bytes[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], bytes[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], bytes[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], bytes[10:16])
	return string(encoded), nil
}

func lookupTypeUntilParentDone(parent context.Context, service string, add dnssd.AddFunc, rmv dnssd.RmvFunc) error {
	return runUntilParentDone(parent, func(ctx context.Context) error {
		return dnssd.LookupType(ctx, service, add, rmv)
	})
}

func runUntilParentDone(parent context.Context, fn func(context.Context) error) error {
	if err := parent.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	stop := context.AfterFunc(parent, cancel)
	defer func() {
		stop()
		cancel()
	}()
	err := fn(ctx)
	if parentErr := parent.Err(); parentErr != nil {
		return parentErr
	}
	return err
}

func Resolve(ctx context.Context, id string) ([]string, error) {
	if !ValidID(id) {
		return nil, errors.New("target ID is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	instances := map[string]string{}
	entryEndpoint := func(entry dnssd.BrowseEntry) (string, bool) {
		if entry.Text["target_id"] != id || entry.Text["protocol"] != protocolVersion || entry.Port < 1 || entry.Port > 65535 {
			return "", false
		}
		var selected net.IP
		for _, ip := range entry.IPs {
			if ip != nil && !ip.IsUnspecified() && ip.To4() != nil {
				selected = ip
				break
			}
		}
		if selected == nil {
			for _, ip := range entry.IPs {
				if ip != nil && !ip.IsUnspecified() {
					selected = ip
					break
				}
			}
		}
		if selected == nil {
			return "", false
		}
		host := selected.String()
		if selected.To4() == nil && selected.IsLinkLocalUnicast() && entry.IfaceName != "" {
			host += "%" + entry.IfaceName
		}
		u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(entry.Port))}
		return u.String(), true
	}
	instanceKey := func(entry dnssd.BrowseEntry) string { return entry.Name + "\x00" + entry.IfaceName }
	err := lookupType(ctx, serviceFQDN, func(entry dnssd.BrowseEntry) {
		endpoint, ok := entryEndpoint(entry)
		if !ok {
			return
		}
		mu.Lock()
		instances[instanceKey(entry)] = endpoint
		mu.Unlock()
	}, func(entry dnssd.BrowseEntry) {
		mu.Lock()
		delete(instances, instanceKey(entry))
		mu.Unlock()
	})
	mu.Lock()
	unique := map[string]struct{}{}
	for _, endpoint := range instances {
		unique[endpoint] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for endpoint := range unique {
		result = append(result, endpoint)
	}
	mu.Unlock()
	sort.Strings(result)
	if len(result) > 0 && (err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return result, nil
	}
	return result, err
}

func Advertise(ctx context.Context, id string, port int) error {
	if !ValidID(id) {
		return errors.New("target ID is invalid")
	}
	if port < 1 || port > 65535 {
		return errors.New("advertisement port is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	config, err := newAdvertisementConfig(id, port)
	if err != nil {
		return err
	}
	retryDelay := advertisementRetryInitial
	for {
		fingerprint := networkFingerprint()
		if fingerprint == "" {
			if err := advertisementWait(ctx, advertisementPollInterval); err != nil {
				return err
			}
			continue
		}
		responder, err := newAdvertiser(config)
		if err != nil {
			if err := advertisementWait(ctx, retryDelay); err != nil {
				return err
			}
			retryDelay = nextRetryDelay(retryDelay)
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- responder.Respond(runCtx) }()
		ticker := time.NewTicker(advertisementPollInterval)
		restart := false
		for !restart {
			select {
			case <-ctx.Done():
				ticker.Stop()
				cancel()
				<-done
				return ctx.Err()
			case <-done:
				ticker.Stop()
				cancel()
				if err := advertisementWait(ctx, retryDelay); err != nil {
					return err
				}
				retryDelay = nextRetryDelay(retryDelay)
				restart = true
			case <-ticker.C:
				if networkFingerprint() != fingerprint {
					ticker.Stop()
					cancel()
					<-done
					retryDelay = advertisementRetryInitial
					restart = true
				}
			}
		}
		// Keep cleanup explicit at the retry boundary as well as on returns.
		cancel()
	}
}

func newAdvertisementConfig(id string, port int) (advertisementConfig, error) {
	var nonce [6]byte
	if _, err := readRandom(nonce[:]); err != nil {
		return advertisementConfig{}, fmt.Errorf("generate advertisement name: %w", err)
	}
	suffix := hex.EncodeToString(nonce[:])
	return advertisementConfig{
		name: "FogCast " + suffix,
		host: "fogcast-" + suffix + ".local.",
		port: port,
		text: []string{"protocol=" + protocolVersion, "target_id=" + id},
	}, nil
}

func usableNetworkFingerprint() string {
	interfaces := usableNetworkInterfaces()
	var state []string
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && !ip.IsUnspecified() && !ip.IsLoopback() {
				state = append(state, fmt.Sprintf("%d|%s|%s", iface.Index, iface.Name, address.String()))
			}
		}
	}
	sort.Strings(state)
	return strings.Join(state, "\n")
}

func usableNetworkInterfaces() []net.Interface {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	usable := interfaces[:0]
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, parseErr := net.ParseCIDR(address.String())
			if parseErr == nil && !ip.IsUnspecified() && !ip.IsLoopback() {
				usable = append(usable, iface)
				break
			}
		}
	}
	return usable
}

func shutdownServers(servers []*mdns.Server) {
	for _, server := range servers {
		_ = server.Shutdown()
	}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextRetryDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > advertisementRetryMaximum {
		return advertisementRetryMaximum
	}
	return delay
}
