// Standalone repro client: connects directly to an RDP server to exercise the
// NLA/CredSSP login path (debugging aid).
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/nakagami/grdp"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	if len(os.Args) < 4 {
		fmt.Println("usage: repro host:port user password [domain]")
		os.Exit(1)
	}
	host, user, pass := os.Args[1], os.Args[2], os.Args[3]
	domain := ""
	if len(os.Args) > 4 {
		domain = os.Args[4]
	}
	c := grdp.NewRdpClient(host, 1024, 768, func(addr string) (net.Conn, error) { return net.DialTimeout("tcp", addr, 10*time.Second) })
	c.OnError(func(err error) { fmt.Println("onError:", err) })
	err := c.Login(domain, user, pass)
	if err != nil {
		fmt.Println("LOGIN FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("LOGIN OK")
	time.Sleep(3 * time.Second)
}
