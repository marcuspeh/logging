package loggingsdk

// Level is the severity of a log event.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "INFO"
	}
}

// ParseLevel parses the canonical level name (case-insensitive). Returns
// (LevelInfo, false) on no match.
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "DEBUG":
		return LevelDebug, true
	case "INFO":
		return LevelInfo, true
	case "WARN":
		return LevelWarn, true
	case "ERROR":
		return LevelError, true
	case "FATAL":
		return LevelFatal, true
	default:
		return LevelInfo, false
	}
}