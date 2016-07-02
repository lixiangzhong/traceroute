package traceroute

import (
	"errors"
	"fmt"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"net"
	"os"
	"time"
)

var (
	ErrRemoteAddr = errors.New("RemoteAddr error")
)

type TraceRoute struct {
	LocalAddr  string //default 0.0.0.0
	RemoteAddr string
	MaxTTL     int           //default 30
	Timeout    time.Duration //default 3 sec
}
type Result struct {
	ID  int
	IP  string
	RTT time.Duration
}

func (r Result) String() string {
	if r.IP == "*" {
		return fmt.Sprintf("%d\t%v", r.ID, r.IP)
	}
	return fmt.Sprintf("%d\t%v\t%.2fms", r.ID, r.IP, (r.RTT.Seconds() * 1000.0))
}

func New(remote string) *TraceRoute {
	return &TraceRoute{
		LocalAddr:  "0.0.0.0",
		RemoteAddr: remote,
		MaxTTL:     30,
		Timeout:    3 * time.Second,
	}
}

func (t *TraceRoute) Do() ([]Result, error) {
	ips, err := net.LookupIP(t.RemoteAddr)
	if err != nil {
		return nil, err
	}
	var dst net.IPAddr
	for _, ip := range ips {
		if ip.To4() != nil {
			dst.IP = ip
			break
		}
	}
	if dst.IP == nil {
		return nil, ErrRemoteAddr
	}
	c, err := net.ListenPacket("ip4:icmp", t.LocalAddr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	p := ipv4.NewPacketConn(c)
	if err := p.SetControlMessage(ipv4.FlagTTL|ipv4.FlagSrc|ipv4.FlagDst|ipv4.FlagInterface, true); err != nil {
		return nil, err
	}
	wm := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0, Body: &icmp.Echo{ID: os.Getpid() & 0xffff, Data: []byte("R-U-OK?")},
	}
	rb := make([]byte, 1500)
	var result []Result
	for i := 1; i < t.MaxTTL; i++ {
		wm.Body.(*icmp.Echo).Seq = i
		wb, err := wm.Marshal(nil)
		if err != nil {
			return result, err
		}
		if err := p.SetTTL(i); err != nil {
			return result, err
		}
		begin := time.Now()
		if _, err := p.WriteTo(wb, nil, &dst); err != nil {
			return result, err
		}
		if err := p.SetReadDeadline(time.Now().Add(t.Timeout)); err != nil {
			return result, err
		}
		n, _, peer, err := p.ReadFrom(rb)
		if err != nil {
			if err, ok := err.(net.Error); ok && err.Timeout() {
				result = append(result, Result{ID: i, IP: "*"})
				continue
			}
			return result, err
		}
		rm, err := icmp.ParseMessage(1, rb[:n])
		if err != nil {
			return result, err
		}
		rtt := time.Since(begin)
		switch rm.Type {
		case ipv4.ICMPTypeTimeExceeded:
			result = append(result, Result{ID: i, IP: peer.String(), RTT: rtt})
		case ipv4.ICMPTypeEchoReply:
			result = append(result, Result{ID: i, IP: peer.String(), RTT: rtt})
			return result, nil
		default:
			result = append(result, Result{ID: i, IP: "*"})
		}
	}
	return result, nil
}
