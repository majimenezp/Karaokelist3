package cdgloader

import (
	"fmt"
	"image"

	"github.com/jpoz/cdgo"
)

const visibleWidth = 288
const visibleHeight = 192

type ExtendedScreen struct {
	internal cdgo.Screen
}

// type ExtendedScreen cdgo.Screen

func (s ExtendedScreen) WriteMemoryBitMap() *image.RGBA {

	img := image.NewRGBA(image.Rect(0, 0, visibleWidth, visibleHeight))
	for x := 0; x < visibleWidth; x++ {
		for y := 0; y < visibleHeight; y++ {
			pixelIndex := x + (y * visibleWidth)

			if pixelIndex > (len(s.internal.Pixels) - 1) {
				fmt.Println("Image index:", pixelIndex, "too bigg")
				continue
			}

			colorIdx := s.internal.Pixels[pixelIndex]
			if colorIdx > uint8(len(s.internal.ColorMap)-1) {
				fmt.Println(pixelIndex, " color too bigg")
				continue
			}

			img.Set(x, y, s.internal.ColorMap[colorIdx])
		}
	}
	return img
}

// type ExtendedScreen struct {
// 	screen cdgo.Screen
// 	Test1  string
// }

func NewExtendedScreen() *ExtendedScreen {
	screen := &ExtendedScreen{
		internal: *cdgo.NewScreen(),
	}
	return screen
}
