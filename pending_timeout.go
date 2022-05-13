package quickfix

import "github.com/ko-matsu/quickfix-go/internal"

type pendingTimeout struct {
	sessionState
}

func (s pendingTimeout) Timeout(session *session, event internal.Event) (nextState sessionState) {
	switch event {
	case internal.PeerTimeout:
		session.log.OnEvent("Session Timeout")
		return latentState{}
	default:
		session.log.OnEventf("receive event: %s, %v", s.String(), event)
	}

	return s
}
