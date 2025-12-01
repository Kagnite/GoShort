package security

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// Compiled regexes for IP obfuscation detection
	octalIPRegex   = regexp.MustCompile(`^0[0-7]+$`)
	hexIPRegex     = regexp.MustCompile(`^0[xX][0-9a-fA-F]+$`)
	decimalIPRegex = regexp.MustCompile(`^\d{7,10}$`)
)

var (
	ErrInvalidScheme        = errors.New("only http/https scheme allowed")
	ErrPrivateAddress       = errors.New("target resolves to private/loopback/internal IP")
	ErrEmptyHost            = errors.New("hostname cannot be empty")
	ErrInvalidURL           = errors.New("invalid URL format")
	ErrBlockedByAllowlist   = errors.New("domain not in allowlist")
	ErrDNSRebindingDetected = errors.New("potential DNS rebinding detected")
	ErrInvalidPort          = errors.New("port not allowed")
	ErrIPLiteralNotAllowed  = errors.New("IP literals not allowed")
	ErrCredentialsInURL     = errors.New("credentials in URL not allowed")
	ErrInvalidHostname      = errors.New("invalid hostname format")
	ErrSuspiciousEncoding   = errors.New("suspicious URL encoding detected")
	ErrCRLFDetected         = errors.New("CRLF characters detected")
)

type SSRFConfig struct {
	AllowedDomains       []string
	UseAllowlist         bool
	AllowedPorts         []int
	MaxRedirects         int
	Timeout              time.Duration
	DisableIPLiterals    bool
	DNSRevalidationCount int
	DNSRevalidationDelay time.Duration
}

type SSRFValidator interface {
	Validate(target string) error
	ValidateWithContext(ctx context.Context, target string) error
	CreateSafeClient() *http.Client
}

type DefaultSSRFValidator struct {
	config   SSRFConfig
	resolver *net.Resolver
}

func NewSSRFValidator(config SSRFConfig) SSRFValidator {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.DNSRevalidationCount == 0 {
		config.DNSRevalidationCount = 2
	}
	if config.DNSRevalidationDelay == 0 {
		config.DNSRevalidationDelay = 100 * time.Millisecond
	}
	if len(config.AllowedPorts) == 0 {
		config.AllowedPorts = []int{80, 443}
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, address)
		},
	}

	return &DefaultSSRFValidator{
		config:   config,
		resolver: resolver,
	}
}

func (v *DefaultSSRFValidator) Validate(target string) error {
	return v.ValidateWithContext(context.Background(), target)
}

func (v *DefaultSSRFValidator) ValidateWithContext(ctx context.Context, target string) error {
	if containsCRLF(target) {
		return ErrCRLFDetected
	}

	if strings.Contains(target, "\x00") {
		return errors.New("null byte detected in URL")
	}

	if err := v.checkSuspiciousEncoding(target); err != nil {
		return err
	}

	normalizedURL, err := v.normalizeURL(target)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	parsed, err := url.Parse(normalizedURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrInvalidScheme
	}

	if parsed.User != nil {
		return ErrCredentialsInURL
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return ErrEmptyHost
	}

	if err := v.validateHostnameFormat(hostname); err != nil {
		return err
	}

	if err := v.checkIPObfuscation(hostname); err != nil {
		return err
	}

	if err := v.validatePort(parsed); err != nil {
		return err
	}

	if v.config.UseAllowlist {
		if !v.isDomainAllowed(hostname) {
			return ErrBlockedByAllowlist
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ips, err := v.resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("DNS resolution failed: %w", err)
	}

	if len(ips) == 0 {
		return errors.New("no IP addresses resolved")
	}

	for _, ipAddr := range ips {
		if v.isBlockedIP(ipAddr.IP) {
			return ErrPrivateAddress
		}
	}

	if err := v.multipleRevalidateDNS(ctx, hostname, ips); err != nil {
		return err
	}

	return nil
}

func containsCRLF(s string) bool {
	return strings.Contains(s, "\r") || strings.Contains(s, "\n")
}

func (v *DefaultSSRFValidator) checkSuspiciousEncoding(target string) error {
	if strings.Contains(target, "%25") {
		decoded, err := url.QueryUnescape(target)
		if err == nil && decoded != target {
			if strings.Contains(decoded, "%") {
				return ErrSuspiciousEncoding
			}
		}
	}

	suspicious := []string{
		"%0d", "%0a", "%0D", "%0A",
		"%250d", "%250a", "%250D", "%250A",
		"\\r", "\\n",
	}
	lowerTarget := strings.ToLower(target)
	for _, pattern := range suspicious {
		if strings.Contains(lowerTarget, pattern) {
			return ErrSuspiciousEncoding
		}
	}

	return nil
}

func (v *DefaultSSRFValidator) normalizeURL(target string) (string, error) {
	target = strings.TrimSpace(target)
	decoded, err := url.QueryUnescape(target)
	if err != nil {
		return target, nil
	}
	doubleDecoded, _ := url.QueryUnescape(decoded)
	if doubleDecoded != decoded {
		return "", errors.New("double URL encoding detected")
	}
	return decoded, nil
}

func (v *DefaultSSRFValidator) validateHostnameFormat(hostname string) error {
	if hostname == "" {
		return ErrEmptyHost
	}
	if len(hostname) > 253 {
		return ErrInvalidHostname
	}
	if strings.Contains(hostname, "[") || strings.Contains(hostname, "]") {
		ip := net.ParseIP(strings.Trim(hostname, "[]"))
		if ip == nil || ip.To16() == nil {
			return fmt.Errorf("%w: invalid brackets usage", ErrInvalidHostname)
		}
	}
	validHostname := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)
	if ip := net.ParseIP(hostname); ip != nil {
		return nil
	}
	if !validHostname.MatchString(hostname) {
		return ErrInvalidHostname
	}
	return nil
}

func (v *DefaultSSRFValidator) checkIPObfuscation(hostname string) error {
	if ip := net.ParseIP(hostname); ip != nil {
		if v.config.DisableIPLiterals {
			return ErrIPLiteralNotAllowed
		}
		return nil
	}
	if strings.HasPrefix(hostname, "[") && strings.HasSuffix(hostname, "]") {
		innerHost := strings.Trim(hostname, "[]")
		if ip := net.ParseIP(innerHost); ip != nil {
			if v.config.DisableIPLiterals {
				return ErrIPLiteralNotAllowed
			}
			if v.isIPv4MappedIPv6(ip) {
				ipv4 := ip.To4()
				if ipv4 != nil && v.isBlockedIP(ipv4) {
					return errors.New("IPv4-mapped IPv6 to blocked address")
				}
			}
			return nil
		}
	}
	if v.isDecimalIP(hostname) {
		return errors.New("decimal IP notation not allowed")
	}
	if v.isHexIP(hostname) {
		return errors.New("hexadecimal IP notation not allowed")
	}
	if v.isOctalIP(hostname) {
		return errors.New("octal IP notation not allowed")
	}
	if v.isShortenedIP(hostname) {
		return errors.New("shortened IP notation not allowed")
	}
	return nil
}

func (v *DefaultSSRFValidator) isIPv4MappedIPv6(ip net.IP) bool {
	if ip.To4() != nil {
		return false
	}
	if len(ip) == 16 {
		for i := 0; i < 10; i++ {
			if ip[i] != 0 {
				return false
			}
		}
		if ip[10] == 0xff && ip[11] == 0xff {
			return true
		}
	}
	return false
}

func (v *DefaultSSRFValidator) isDecimalIP(hostname string) bool {
	// Use pre-compiled regex for better performance
	if decimalIPRegex.MatchString(hostname) {
		if num, err := strconv.ParseUint(hostname, 10, 32); err == nil {
			ip := make(net.IP, 4)
			ip[0] = byte(num >> 24)
			ip[1] = byte(num >> 16)
			ip[2] = byte(num >> 8)
			ip[3] = byte(num)
			return ip != nil
		}
	}
	return false
}

func (v *DefaultSSRFValidator) isHexIP(hostname string) bool {
	if strings.HasPrefix(hostname, "0x") || strings.HasPrefix(hostname, "0X") {
		hexStr := strings.TrimPrefix(strings.ToLower(hostname), "0x")
		if _, err := hex.DecodeString(hexStr); err == nil {
			return true
		}
	}
	// Use pre-compiled regex for better performance
	if hexIPRegex.MatchString(hostname) {
		return true
	}
	return false
}

func (v *DefaultSSRFValidator) isOctalIP(hostname string) bool {
	parts := strings.Split(hostname, ".")
	if len(parts) >= 1 && len(parts) <= 4 {
		for _, part := range parts {
			if strings.HasPrefix(part, "0") && len(part) > 1 {
				// Use pre-compiled regex instead of regexp.MatchString in loop
				if octalIPRegex.MatchString(part) {
					return true
				}
			}
		}
	}
	return false
}

func (v *DefaultSSRFValidator) isShortenedIP(hostname string) bool {
	parts := strings.Split(hostname, ".")
	if len(parts) > 0 && len(parts) < 4 {
		allNumeric := true
		for _, part := range parts {
			if _, err := strconv.Atoi(part); err != nil {
				allNumeric = false
				break
			}
		}
		if allNumeric {
			return true
		}
	}
	return false
}

func (v *DefaultSSRFValidator) validatePort(parsed *url.URL) error {
	portStr := parsed.Port()
	var port int
	var err error
	if portStr == "" {
		if parsed.Scheme == "http" {
			port = 80
		} else if parsed.Scheme == "https" {
			port = 443
		}
	} else {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return fmt.Errorf("invalid port: %w", err)
		}
	}
	allowed := false
	for _, allowedPort := range v.config.AllowedPorts {
		if port == allowedPort {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: port %d not in allowed list", ErrInvalidPort, port)
	}
	return nil
}

func (v *DefaultSSRFValidator) isDomainAllowed(hostname string) bool {
	hostname = strings.ToLower(hostname)
	for _, allowed := range v.config.AllowedDomains {
		allowed = strings.ToLower(allowed)
		if hostname == allowed {
			return true
		}
		if strings.HasPrefix(allowed, "*.") {
			domain := strings.TrimPrefix(allowed, "*.")
			if strings.HasSuffix(hostname, "."+domain) || hostname == domain {
				return true
			}
		}
	}
	return false
}

func (v *DefaultSSRFValidator) isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	blockedIPs := []string{
		"169.254.169.254", "169.254.169.253", "fd00:ec2::254", "fe80::5efe:169.254.169.254",
	}
	for _, blocked := range blockedIPs {
		if blockIP := net.ParseIP(blocked); blockIP != nil && blockIP.Equal(ip) {
			return true
		}
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 0 || (ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127) ||
			(ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0) ||
			(ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2) ||
			(ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100) ||
			(ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113) ||
			ip4[0] >= 240 || ip4[3] == 255 {
			return true
		}
	}
	if ip.To4() == nil && ip.To16() != nil {
		if ip[0] == 0xfc || ip[0] == 0xfd {
			return true
		}
	}
	return false
}

func (v *DefaultSSRFValidator) multipleRevalidateDNS(ctx context.Context, hostname string, firstIPs []net.IPAddr) error {
	for i := 0; i < v.config.DNSRevalidationCount; i++ {
		time.Sleep(v.config.DNSRevalidationDelay)
		revalidatedIPs, err := v.resolver.LookupIPAddr(ctx, hostname)
		if err != nil {
			return fmt.Errorf("DNS revalidation %d failed: %w", i+1, err)
		}
		if !v.compareIPLists(firstIPs, revalidatedIPs) {
			return fmt.Errorf("%w: IP changed during revalidation %d", ErrDNSRebindingDetected, i+1)
		}
		for _, ipAddr := range revalidatedIPs {
			if v.isBlockedIP(ipAddr.IP) {
				return fmt.Errorf("%w: blocked IP detected during revalidation", ErrPrivateAddress)
			}
		}
	}
	return nil
}

func (v *DefaultSSRFValidator) compareIPLists(ips1, ips2 []net.IPAddr) bool {
	if len(ips1) != len(ips2) {
		return false
	}
	ipMap := make(map[string]bool)
	for _, ip := range ips1 {
		ipMap[ip.IP.String()] = true
	}
	for _, ip := range ips2 {
		if !ipMap[ip.IP.String()] {
			return false
		}
	}
	return true
}

func (v *DefaultSSRFValidator) CreateSafeClient() *http.Client {
	return &http.Client{
		Timeout: v.config.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if v.config.MaxRedirects == 0 {
				return http.ErrUseLastResponse
			}
			if len(via) >= v.config.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", v.config.MaxRedirects)
			}
			if err := v.Validate(req.URL.String()); err != nil {
				return fmt.Errorf("redirect target blocked: %w", err)
			}
			return nil
		},
		Transport: &http.Transport{
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			DisableKeepAlives:     true,
			MaxIdleConnsPerHost:   1,
			ResponseHeaderTimeout: v.config.Timeout,
			ExpectContinueTimeout: 1 * time.Second,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := v.resolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("DNS resolution failed during dial: %w", err)
				}
				for _, ipAddr := range ips {
					if v.isBlockedIP(ipAddr.IP) {
						return nil, ErrPrivateAddress
					}
				}
				dialer := &net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: -1,
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
}
