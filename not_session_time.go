package quickfix

import "github.com/ko-matsu/quickfix-go/internal"

type notSessionTime struct{ latentState }

func (notSessionTime) String() string      { return "Not session time" }
func (notSessionTime) IsSessionTime() bool { return false }

func (state notSessionTime) FixMsgIn(session *session, msg *Message) (nextState sessionState) {
	session.log.OnEventf("Invalid Session State: Unexpected Msg %v while in Latent state", msg)
	return state
}

func (state notSessionTime) Timeout(session *session, event internal.Event) (nextState sessionState) {
	session.log.OnEventf("receive event: %s, %v", state.String(), event)
	return state
}

func (state notSessionTime) Stop(*session) (nextState sessionState) {
	return state
}
