package quickfix

import "github.com/cryptogarageinc/quickfix-go/internal"

type pendingTimeout struct {
	sessionState
}

func (s pendingTimeout) Timeout(session *session, event internal.Event) (nextState sessionState) {
	switch event {
	case internal.PeerTimeout:
		session.log.OnEvent("Session Timeout")
		return latentState{}
	default:
		session.log.OnEventParams("receive event",
			LogString("sessionState", s.String()), LogObject("event", event))
	}

	return s
}
