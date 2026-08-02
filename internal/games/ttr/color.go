// Package ttr implements Ticket to Ride: Europe as an engine.Engine. See
// rules/ticket_to_ride_europe.md for the full rules specification this
// package implements.
package ttr

// Color is a train-card colour or a route colour (rules §3.3). Gray is a
// route colour only — it is never dealt as a train card. Locomotive is a
// card colour only — it is never printed on a route.
type Color int8

// Color values. ColorUnspecified is the zero value and never denotes a real
// card or route.
const (
	ColorUnspecified Color = iota
	ColorPurple
	ColorBlue
	ColorOrange
	ColorWhite
	ColorGreen
	ColorYellow
	ColorBlack
	ColorRed
	ColorGray
	ColorLoco
)

// colorNames maps every real Color to its canonical string form, used by
// both String and map-document JSON.
var colorNames = map[Color]string{
	ColorPurple: "Purple",
	ColorBlue:   "Blue",
	ColorOrange: "Orange",
	ColorWhite:  "White",
	ColorGreen:  "Green",
	ColorYellow: "Yellow",
	ColorBlack:  "Black",
	ColorRed:    "Red",
	ColorGray:   "Gray",
	ColorLoco:   "Locomotive",
}

// colorByName is the reverse of colorNames, used by ParseColor.
var colorByName = map[string]Color{
	"Purple":     ColorPurple,
	"Blue":       ColorBlue,
	"Orange":     ColorOrange,
	"White":      ColorWhite,
	"Green":      ColorGreen,
	"Yellow":     ColorYellow,
	"Black":      ColorBlack,
	"Red":        ColorRed,
	"Gray":       ColorGray,
	"Locomotive": ColorLoco,
}

// String renders c using the canonical names used in map documents and the
// wire protocol ("Purple" … "Gray", "Locomotive"). ColorUnspecified and any
// out-of-range value render as "Unspecified".
func (c Color) String() string {
	if s, ok := colorNames[c]; ok {
		return s
	}
	return "Unspecified"
}

// ParseColor parses one of the canonical color names (as used in map
// documents) into a Color. ok is false for unknown names, including
// "Unspecified".
func ParseColor(s string) (Color, bool) {
	c, ok := colorByName[s]
	return c, ok
}

// IsCardColor reports whether c can appear as a train card: the 8 payable
// colours plus Locomotive. Gray and Unspecified are never card colours.
func (c Color) IsCardColor() bool {
	_, ok := DeckComposition[c]
	return ok
}

// IsRouteColor reports whether c can appear as a route colour: the 8
// payable colours plus Gray. Locomotive and Unspecified are never route
// colours.
func (c Color) IsRouteColor() bool {
	return c != ColorUnspecified && c != ColorLoco
}

// CardColors is the 8 payable train-card colours in canonical order. It
// excludes Gray (route-only) and Locomotive (counted separately).
var CardColors = [...]Color{
	ColorPurple, ColorBlue, ColorOrange, ColorWhite,
	ColorGreen, ColorYellow, ColorBlack, ColorRed,
}

// DeckComposition is the count of each card colour in the 110-card train
// deck (rules §3.2): 12 of each of the 8 colours plus 14 Locomotives.
var DeckComposition = map[Color]int{
	ColorPurple: 12,
	ColorBlue:   12,
	ColorOrange: 12,
	ColorWhite:  12,
	ColorGreen:  12,
	ColorYellow: 12,
	ColorBlack:  12,
	ColorRed:    12,
	ColorLoco:   14,
}

// TotalTrainCards is the total number of cards in the train deck (rules
// §3.2): 8*12 + 14 = 110.
const TotalTrainCards = 110
