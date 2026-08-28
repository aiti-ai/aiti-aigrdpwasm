// Standalone repro client: connects directly to an RDP server to exercise the
// NLA/CredSSP login path (debugging aid).
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/nakagami/grdp"
)

var start = time.Now()

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
	c := grdp.NewRdpClient(host, 1280, 800, func(addr string) (net.Conn, error) { return net.DialTimeout("tcp", addr, 10*time.Second) })
	// Mirror the page's AVC-advertisement path (the wasm build registers OnH264Raw,
	// so CAPS_ADVERTISE declares AVC420 support — which makes Windows send ClearCodec).
	c.OnH264Raw(func(dx, dy, w, h int, isKey bool, data []byte) { fmt.Println("H264 frame", dx, dy, w, h, "key=", isKey, "len=", len(data)) })
	n := 0
	c.OnBitmap(func(bs []grdp.Bitmap) {
		for _, b := range bs {
			fmt.Printf("BMP t=%.1f L=%d T=%d R=%d B=%d W=%d H=%d dataLen=%d\n",
				time.Since(start).Seconds(), b.DestLeft, b.DestTop, b.DestRight, b.DestBottom, b.Width, b.Height, len(b.Data))
			if n < 12 && b.BitsPerPixel == 4 {
				img := image.NewRGBA(image.Rect(0, 0, b.Width, b.Height))
				for y := 0; y < b.Height; y++ {
					for x := 0; x < b.Width; x++ {
						i := (y*b.Width + x) * 4
						if i+3 < len(b.Data) {
							img.SetRGBA(x, y, color.RGBA{R: b.Data[i+2], G: b.Data[i+1], B: b.Data[i], A: 0xFF})
						}
					}
				}
				f, _ := os.Create(fmt.Sprintf("/tmp/repro-bmp-%d-L%d-T%d.png", n, b.DestLeft, b.DestTop))
				png.Encode(f, img)
				f.Close()
				n++
			}
		}
	})
	c.OnError(func(err error) { fmt.Println("onError:", err) })
	err := c.Login(domain, user, pass)
	if err != nil {
		fmt.Println("LOGIN FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("LOGIN OK")
	time.Sleep(30 * time.Second)
}
