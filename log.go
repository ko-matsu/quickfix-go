package quickfix

import (
	"bytes"
	"fmt"
)

func makeReadable(s []byte) string {
	return string(bytes.Replace(s, []byte("\x01"), []byte("|"), -1))
}

type LogParam struct {
	Name   string
	Value  interface{}
	format string
}

func (l LogParam) GetFormat() string {
	return l.format
}

func (l LogParam) String() string {
	switch l.format {
	case "%s":
		return fmt.Sprintf("%s=%s", l.Name, l.Value)
	case "%q":
		return fmt.Sprintf("%s=%q", l.Name, l.Value)
	case "%d":
		return fmt.Sprintf("%s=%d", l.Name, l.Value)
	case "%+v":
		return fmt.Sprintf("%s=%+v", l.Name, l.Value)
	default:
		return fmt.Sprintf("%s=%v", l.Name, l.Value)
	}
}

func LogMessage(name string, msgBytes []byte) LogParam {
	return LogParam{
		Name:   name,
		Value:  makeReadable(msgBytes),
		format: "%s",
	}
}

func LogString(name, value string) LogParam {
	return LogParam{
		Name:   name,
		Value:  value,
		format: "%s",
	}
}

func LogStringWithSingleQuote(name, value string) LogParam {
	return LogParam{
		Name:   name,
		Value:  value,
		format: "%q",
	}
}

func LogInt(name string, value int) LogParam {
	return LogParam{
		Name:   name,
		Value:  value,
		format: "%d",
	}
}

func LogInt64(name string, value int64) LogParam {
	return LogParam{
		Name:   name,
		Value:  value,
		format: "%d",
	}
}

func LogUint64(name string, value uint64) LogParam {
	return LogParam{
		Name:   name,
		Value:  value,
		format: "%d",
	}
}

func LogObject(name string, value interface{}) LogParam {
	return LogParam{
		Name:   name,
		Value:  value,
		format: "%v",
	}
}

// Log is a generic interface for logging FIX messages and events.
type Log interface {
	//log incoming fix message
	OnIncoming([]byte)

	//log outgoing fix message
	OnOutgoing([]byte)

	//log fix event
	OnEvent(string)

	//log fix event according to format specifier
	OnEventf(string, ...interface{})

	//log fix error event
	OnErrorEvent(string, error)

	//log fix event according to logging parameter
	OnEventParams(string, ...LogParam)

	//log fix error event according to logging parameter
	OnErrorEventParams(string, error, ...LogParam)
}

// The LogFactory interface creates global and session specific Log instances
type LogFactory interface {
	//global log
	Create() (Log, error)

	//session specific log
	CreateSessionLog(sessionID SessionID) (Log, error)
}
