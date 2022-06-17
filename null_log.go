package quickfix

type nullLog struct{}

func (l nullLog) OnIncoming([]byte)                                           {}
func (l nullLog) OnOutgoing([]byte)                                           {}
func (l nullLog) OnEvent(string)                                              {}
func (l nullLog) OnEventf(format string, a ...interface{})                    {}
func (l nullLog) OnErrorEvent(message string, err error)                      {}
func (l nullLog) OnEventParams(message string, v ...LogParam)                 {}
func (l nullLog) OnErrorEventParams(message string, err error, v ...LogParam) {}

type nullLogFactory struct{}

func (nullLogFactory) Create() (Log, error) {
	return nullLog{}, nil
}
func (nullLogFactory) CreateSessionLog(sessionID SessionID) (Log, error) {
	return nullLog{}, nil
}

//NewNullLogFactory creates an instance of LogFactory that returns no-op loggers.
func NewNullLogFactory() LogFactory {
	return nullLogFactory{}
}
