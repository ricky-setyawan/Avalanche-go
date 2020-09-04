// (c) 2020, Alex Willmer, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh/terminal"
)

// Highlighting mode to apply to displayed logs
type Highlight int

// Highlighting modes available
const (
	Plain Highlight = iota
	Colors
)

// Choose a highlighting mode
func ToHighlight(h string, fd uintptr) (Highlight, error) {
	switch strings.ToUpper(h) {
	case "PLAIN":
		return Plain, nil
	case "COLORS":
		return Colors, nil
	case "AUTO":
		if !terminal.IsTerminal(int(fd)) {
			return Plain, nil
		} else {
			return Colors, nil
		}
	default:
		return Plain, fmt.Errorf("unknown highlight mode: %s", h)
	}
}
