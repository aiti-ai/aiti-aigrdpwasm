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
	c.DisableAVC444() // 与 wasm main.go 对齐：只广告 v8.0+v8.1（AVC420），观察 Windows 实际发送的 codec
	if os.Getenv("REPRO_NOGFX") != "" {
		c.DisableGFX() // legacy 位图模式（模拟页面 ?nogfx=1）
	}
	c.OnH264Raw(func(dx, dy, w, h int, isKey bool, data []byte) { fmt.Println("H264 frame", dx, dy, w, h, "key=", isKey, "len=", len(data)) })
	n := 0
	// Full-canvas compositing: paint every block onto a 1280x800 surface.
	canvasImg := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	c.OnBitmap(func(bs []grdp.Bitmap) {
		for _, b := range bs {
			fmt.Printf("BMP t=%.1f L=%d T=%d R=%d B=%d W=%d H=%d bpp=%d dataLen=%d\n",
				time.Since(start).Seconds(), b.DestLeft, b.DestTop, b.DestRight, b.DestBottom, b.Width, b.Height, b.BitsPerPixel, len(b.Data))
			if n <= 6 {
				// 保存前几个原始 tile 供离线检查
				tileImg := image.NewRGBA(image.Rect(0, 0, b.Width, b.Height))
				for y := 0; y < b.Height; y++ {
					for x := 0; x < b.Width; x++ {
						i := (y*b.Width + x) * 4
						if i+3 < len(b.Data) {
							tileImg.SetRGBA(x, y, color.RGBA{b.Data[i+2], b.Data[i+1], b.Data[i], 0xFF})
						}
					}
				}
				tf, _ := os.Create(fmt.Sprintf("/tmp/legacy-tile-%d_%dx%d_at%d_%d.png", n, b.Width, b.Height, b.DestLeft, b.DestTop))
				png.Encode(tf, tileImg)
				tf.Close()
			}
			if b.BitsPerPixel == 4 {
				for y := 0; y < b.Height && b.DestTop+y < 800; y++ {
					for x := 0; x < b.Width && b.DestLeft+x < 1280; x++ {
						i := (y*b.Width + x) * 4
						if i+3 < len(b.Data) {
							di := ((b.DestTop+y)*1280 + (b.DestLeft + x)) * 4
							canvasImg.Pix[di] = b.Data[i+2]
							canvasImg.Pix[di+1] = b.Data[i+1]
							canvasImg.Pix[di+2] = b.Data[i]
							canvasImg.Pix[di+3] = 0xFF
						}
					}
				}
			}
			n++
		}
		if n%5 == 0 || n == 1 {
			f, _ := os.Create(fmt.Sprintf("/tmp/repro-full-%d.png", n))
			png.Encode(f, canvasImg)
			f.Close()
		}
	})
	c.OnError(func(err error) { fmt.Println("onError:", err) })

	// Auto-unlock: send password after connect
	go func() {
		time.Sleep(4 * time.Second)
		keys := []int{0x2C, 0x12, 0x1F, 0x2C, 0x02, 0x03, 0x04, 0x1C}
		for _, sc := range keys {
			c.KeyDown(sc)
			time.Sleep(60 * time.Millisecond)
			c.KeyUp(sc)
			time.Sleep(60 * time.Millisecond)
		}
		fmt.Println("AUTO-UNLOCK sent")
	}()

	err := c.Login(domain, user, pass)
	if err != nil {
		fmt.Println("LOGIN FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("LOGIN OK")

	// 刺激 UI 变化以捕获 TILE_UPGRADE 帧：Ctrl+Esc 打开开始菜单，
	// 数秒后 Esc 关闭，重复两次。
	go func() {
		time.Sleep(8 * time.Second)
		for round := 0; round < 2; round++ {
			fmt.Println("STIM: Ctrl+Esc (start menu)")
			c.KeyDown(0x1D) // Ctrl
			c.KeyDown(0x01) // Esc
			time.Sleep(120 * time.Millisecond)
			c.KeyUp(0x01)
			c.KeyUp(0x1D)
			time.Sleep(9 * time.Second)
			c.KeyDown(0x01) // Esc 关闭菜单
			time.Sleep(100 * time.Millisecond)
			c.KeyUp(0x01)
			time.Sleep(6 * time.Second)
		}
	}()

	time.Sleep(55 * time.Second)
}
