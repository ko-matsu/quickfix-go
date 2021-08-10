package quickfix

import (
	"errors"
	"sync"
	"time"
)

var sessionsLock sync.RWMutex
var sessions = make(map[SessionID]*session)
var errDuplicateSessionID = errors.New("Duplicate SessionID")
var errUnknownSession = errors.New("Unknown session")

//Messagable is a Message or something that can be converted to a Message
type Messagable interface {
	ToMessage() *Message
}

//Send determines the session to send Messagable using header fields BeginString, TargetCompID, SenderCompID
func Send(m Messagable) (err error) {
	msg := m.ToMessage()
	var beginString FIXString
	if err := msg.Header.GetField(tagBeginString, &beginString); err != nil {
		return err
	}

	var targetCompID FIXString
	if err := msg.Header.GetField(tagTargetCompID, &targetCompID); err != nil {
		return err
	}

	var senderCompID FIXString
	if err := msg.Header.GetField(tagSenderCompID, &senderCompID); err != nil {

		return nil
	}

	sessionID := SessionID{BeginString: string(beginString), TargetCompID: string(targetCompID), SenderCompID: string(senderCompID)}

	return SendToTarget(msg, sessionID)
}

//SendToTarget sends a message based on the sessionID. Convenient for use in FromApp since it provides a session ID for incoming messages
func SendToTarget(m Messagable, sessionID SessionID) error {
	msg := m.ToMessage()
	session, ok := lookupSession(sessionID)
	if !ok {
		return errUnknownSession
	}

	return session.queueForSend(msg)
}

//UnregisterSession removes a session from the set of known sessions
func UnregisterSession(sessionID SessionID) error {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	if s, ok := sessions[sessionID]; ok {
		registerStoppedSession(s)
		delete(sessions, sessionID)
		return nil
	}

	return errUnknownSession
}

func registerSession(s *session) error {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	if _, ok := sessions[s.sessionID]; ok {
		return errDuplicateSessionID
	}

	sessions[s.sessionID] = s
	unregisterStoppedSession(s.sessionID)
	return nil
}

func lookupSession(sessionID SessionID) (s *session, ok bool) {
	sessionsLock.RLock()
	defer sessionsLock.RUnlock()

	s, ok = sessions[sessionID]
	return
}

// append API ------------------------------------------------------------------

const (
	// DoNotLoggedOnSessionMessage This message is use by SendToTarget.
	doNotLoggedOnSessionMessage = "session is not loggedOn"
)

// ErrDoNotLoggedOnSession defines no loggedOn session error.
var ErrDoNotLoggedOnSession = errors.New(doNotLoggedOnSessionMessage)

var stoppedSessionsLock sync.RWMutex
var stoppedSessions = make(map[SessionID]*session)
var isClosedStopeedSessions = false

// ErrorBySessionID This struct has error map by sessionID.
type ErrorBySessionID struct {
	error
	ErrorMap map[SessionID]error
}

// NewErrorBySessionID This function returns NewErrorBySessionID object.
func NewErrorBySessionID(err error) (response *ErrorBySessionID) {
	response = &ErrorBySessionID{error: err, ErrorMap: make(map[SessionID]error)}
	return response
}

// GetSessionIDs This function returns sessionID list.
func GetSessionIDs() []SessionID {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	sessionIds := make([]SessionID, 0, len(sessions))
	for sessionID := range sessions {
		sessionIds = append(sessionIds, sessionID)
	}
	return sessionIds
}

// GetAliveSessionIDs This function returns loggedOn sessionID list.
func GetAliveSessionIDs() []SessionID {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	sessionIds := make([]SessionID, 0, len(sessions))
	for sessionID, session := range sessions {
		if session.IsLoggedOn() {
			sessionIds = append(sessionIds, sessionID)
		}
	}
	return sessionIds
}

// IsAliveSession This function checks if the session is a logged on session or not.
func IsAliveSession(sessionID SessionID) bool {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	session, ok := sessions[sessionID]
	if ok {
		return session.IsLoggedOn()
	}
	return false
}

// SendToAliveSession This function send message for logged on session.
func SendToAliveSession(m Messagable, sessionID SessionID) (err error) {
	if !IsAliveSession(sessionID) {
		err = ErrDoNotLoggedOnSession
	} else {
		err = SendToTarget(m, sessionID)
	}
	return err
}

// SendToAliveSessions This function send messages for logged on sessions.
func SendToAliveSessions(m Messagable) (err error) {
	sessionIDs := GetAliveSessionIDs()
	err = sendToSessions(m, sessionIDs)
	if err != nil {
		errObj := err.(*ErrorBySessionID)
		errObj.error = errors.New("failed to SendToAliveSessions")
	}
	return err
}

func sendToSessions(m Messagable, sessionIDs []SessionID) (err error) {
	errorByID := ErrorBySessionID{ErrorMap: make(map[SessionID]error)}
	baseMsg := m.ToMessage()
	for _, sessionID := range sessionIDs {
		msg := NewMessage()
		baseMsg.CopyInto(msg)
		msg = fillHeaderBySessionID(msg, sessionID)
		tmpErr := SendToAliveSession(msg, sessionID)
		if tmpErr != nil {
			errorByID.ErrorMap[sessionID] = tmpErr
		}
	}
	if len(errorByID.ErrorMap) > 0 {
		err = &errorByID
		errorByID.error = errors.New("failed to SendToSessions")
	}
	return err
}

func fillHeaderBySessionID(m *Message, sessionID SessionID) *Message {
	if sessionID.BeginString != "" {
		m.Header.SetField(tagBeginString, FIXString(sessionID.BeginString))
	}
	if sessionID.SenderCompID != "" {
		m.Header.SetField(tagSenderCompID, FIXString(sessionID.SenderCompID))
	}
	if sessionID.TargetCompID != "" {
		m.Header.SetField(tagTargetCompID, FIXString(sessionID.TargetCompID))
	}
	if sessionID.SenderSubID != "" {
		m.Header.SetField(tagSenderSubID, FIXString(sessionID.SenderSubID))
	}
	if sessionID.SenderLocationID != "" {
		m.Header.SetField(tagSenderLocationID, FIXString(sessionID.SenderLocationID))
	}
	if sessionID.TargetSubID != "" {
		m.Header.SetField(tagTargetSubID, FIXString(sessionID.TargetSubID))
	}
	if sessionID.TargetLocationID != "" {
		m.Header.SetField(tagTargetLocationID, FIXString(sessionID.TargetLocationID))
	}
	return m
}

// WaitForLogon returns channel to receive logon event by specific session. if non-existing sessionID specified, it returns nil which blocks forever.
func WaitForLogon(sessionID SessionID) <-chan struct{} {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	if session, ok := sessions[sessionID]; ok {
		return session.notifyLogonEvent
	}
	return nil // fail case
}

// SendToSession This function send message on session.
// If the session is stopped, It just saves the message without sending it.
func SendToSession(m Messagable, sessionID SessionID) (err error) {
	msg := m.ToMessage()
	session, ok := lookupSession(sessionID)
	if ok {
		return session.queueForSend(msg)
	}
	session, ok = lookupStoppedSession(sessionID)
	if ok {
		return session.queueForSend(msg)
	}
	return errUnknownSession
}

func registerStoppedSession(s *session) {
	if isClosedStopeedSessions || s.stoppedSessionKeepTime == 0 {
		return
	}

	stoppedSessionsLock.Lock()
	defer stoppedSessionsLock.Unlock()
	deleteOldStoppedSession()

	if oldSession, ok := stoppedSessions[s.sessionID]; ok {
		delete(stoppedSessions, s.sessionID)
		oldSession.close()
	}
	stoppedSessions[s.sessionID] = s
}

func unregisterStoppedSession(sessionID SessionID) {
	stoppedSessionsLock.Lock()
	defer stoppedSessionsLock.Unlock()
	deleteOldStoppedSession()

	if s, ok := stoppedSessions[sessionID]; ok {
		delete(stoppedSessions, sessionID)
		s.close()
	}
}

func unregisterStoppedSessionAll() {
	stoppedSessionsLock.Lock()
	defer stoppedSessionsLock.Unlock()

	for id, stoppedSession := range stoppedSessions {
		stoppedSession.close()
		delete(stoppedSessions, id)
	}
	isClosedStopeedSessions = true
}

func lookupStoppedSession(sessionID SessionID) (s *session, ok bool) {
	stoppedSessionsLock.RLock()
	defer stoppedSessionsLock.RUnlock()

	s, ok = stoppedSessions[sessionID]
	return
}

func deleteOldStoppedSession() { // in-file function
	if len(stoppedSessions) == 0 {
		return
	}
	currentTime := time.Now().UTC()
	for id, stoppedSession := range stoppedSessions {
		if stoppedSession.stoppedSessionKeepTime < 0 {
			continue
		}
		diffTime := currentTime.Sub(stoppedSession.stopTime)
		if diffTime > stoppedSession.stoppedSessionKeepTime {
			stoppedSession.close()
			delete(stoppedSessions, id)
		}
	}
}
