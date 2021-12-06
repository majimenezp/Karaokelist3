package cdgloader

import (
	"image"
	"image/color"
	"log"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/jpoz/cdgo"
	"github.com/nsf/termbox-go"
)

type Loader interface {
	LoadFile(string) (bool, error)
}

type CDGLoader struct {
}

func (l *CDGLoader) LoadFile(filepath string) (bool, error) {
	file, err := os.Open(filepath)
	check(err)
	cdgFile := cdgo.NewFile(file)
	screen := NewExtendedScreen()
	imagesList := []image.Image{}
	start := time.Now()
	for {
		packet, err := cdgFile.NextPacket()
		if err != nil {
			log.Fatal(err)
			break
		}
		if packet.IsCgdCommand() {
			err = screen.internal.UpdateWithPacket(packet)
			if err != nil {
				log.Fatal(err)
				break
			}

			if screen.internal.Dirty {
				test1 := screen.WriteMemoryBitMap()
				imagesList = append(imagesList, test1)
				screen.internal.Dirty = false
			}
		}
	}
	elapsed := time.Since(start)
	log.Printf("CDG dump took %s", elapsed)
	return true, nil
}

func drawImage(img image.Image) {
	width, height, whratio := canvasSize()

	bounds := img.Bounds()
	imgW, imgH := bounds.Dx(), bounds.Dy()
	imgScale := scale(imgW, imgH, width, height, whratio)
	width, height = int(float64(imgW)/imgScale), int(float64(imgH)/(imgScale*whratio))

	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Calculate average color for the corresponding image rectangle
			// fitting in this cell. We use a half-block trick, wherein the
			// lower half of the cell displays the character ▄, effectively
			// doubling the resolution of the canvas.
			startX, startY, endX, endY := imgArea(x, y, imgScale, whratio)

			r, g, b := avgRGB(img, startX, startY, endX, (startY+endY)/2)
			colorUp := termbox.Attribute(termColor(r, g, b))

			r, g, b = avgRGB(img, startX, (startY+endY)/2, endX, endY)
			colorDown := termbox.Attribute(termColor(r, g, b))

			termbox.SetCell(x, y, '▄', colorDown, colorUp)
		}
	}
	termbox.Flush()
}

const defaultRatio float64 = 7.0 / 3.0 // The terminal's default cursor width/height ratio

func canvasSize() (int, int, float64) {
	var size [4]uint16
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(os.Stdout.Fd()), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)), 0, 0, 0); err != 0 {
		panic(err)
	}
	rows, cols, width, height := size[0], size[1], size[2], size[3]

	var whratio = defaultRatio
	if width > 0 && height > 0 {
		whratio = float64(height/rows) / float64(width/cols)
	}

	return int(cols), int(rows), whratio
}

func scale(imgW, imgH, termW, termH int, whratio float64) float64 {
	hr := float64(imgH) / (float64(termH) * whratio)
	wr := float64(imgW) / float64(termW)
	return max(hr, wr, 1)
}

func imgArea(termX, termY int, imgScale, whratio float64) (int, int, int, int) {
	startX, startY := float64(termX)*imgScale, float64(termY)*imgScale*whratio
	endX, endY := startX+imgScale, startY+imgScale*whratio

	return int(startX), int(startY), int(endX), int(endY)
}

// avgRGB calculates the average RGB color within the given
// rectangle, and returns the [0,1] range of each component.
func avgRGB(img image.Image, startX, startY, endX, endY int) (uint16, uint16, uint16) {
	var total = [3]uint16{}
	var count uint16
	for x := startX; x < endX; x++ {
		for y := startY; y < endY; y++ {
			if (!image.Point{x, y}.In(img.Bounds())) {
				continue
			}
			r, g, b := rgb(img.At(x, y))
			total[0] += r
			total[1] += g
			total[2] += b
			count++
		}
	}

	r := total[0] / count
	g := total[1] / count
	b := total[2] / count
	return r, g, b
}

func max(values ...float64) float64 {
	var m float64
	for _, v := range values {
		if v > m {
			m = v
		}
	}
	return m
}

func rgb(c color.Color) (uint16, uint16, uint16) {
	r, g, b, _ := c.RGBA()
	// Reduce color values to the range [0, 15]
	return uint16(r >> 8), uint16(g >> 8), uint16(b >> 8)
}

// termColor converts a 24-bit RGB color into a term256 compatible approximation.
func termColor(r, g, b uint16) uint16 {
	rterm := (((r * 5) + 127) / 255) * 36
	gterm := (((g * 5) + 127) / 255) * 6
	bterm := (((b * 5) + 127) / 255)

	return rterm + gterm + bterm + 16 + 1 // termbox default color offset
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
