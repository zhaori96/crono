package buffer

type AccessMode int

const (
	AccessModeSynchronous AccessMode = iota
	AccessModeAsynchronous
	AccessModeStrategic
)

func (mode AccessMode) String() string {
	switch mode {
	case AccessModeSynchronous:
		return "Synchronous"
	case AccessModeAsynchronous:
		return "Asynchronous"
	case AccessModeStrategic:
		return "Strategic"
	default:
		return "Unknown"
	}
}
