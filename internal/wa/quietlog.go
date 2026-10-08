package wa

import (
	"fmt"
	"strings"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// quietLog logs app state sync failures as warnings. Some accounts have
// settings history (pins, mutes, archive) that never verifies; it doesn't
// affect messages, so it shouldn't show up as an error on every link.
type quietLog struct{ waLog.Logger }

func (l quietLog) Errorf(format string, args ...any) {
	if isAppStateNoise(fmt.Sprintf(format, args...)) {
		l.Logger.Warnf(format, args...)
		return
	}
	l.Logger.Errorf(format, args...)
}

func (l quietLog) Sub(module string) waLog.Logger { return quietLog{l.Logger.Sub(module)} }

func isAppStateNoise(msg string) bool {
	return strings.Contains(msg, "app state") && strings.Contains(msg, "mismatching LTHash")
}
