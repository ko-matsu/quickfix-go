package quickfix

import "github.com/cryptogarageinc/quickfix-go/internal"

type latentState struct{ inSessionTime }

func (state latentState) String() string    { return "Latent State" }
func (state latentState) IsLoggedOn() bool  { return false }
func (state latentState) IsConnected() bool { return false }

func (state latentState) FixMsgIn(session *session, msg *Message) (nextState sessionState) {
	session.log.OnEventf("Invalid Session State: Unexpected Msg %v while in Latent state", msg)
	return state
}

func (state latentState) Timeout(session *session, event internal.Event) (nextState sessionState) {
	session.log.OnEventf("receive event: %s, %v", state.String(), event)
	return state
}

func (state latentState) ShutdownNow(*session) {}
func (state latentState) Stop(*session) (nextState sessionState) {
	return state
}
