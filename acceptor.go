package quickfix

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"runtime/debug"
	"strconv"
	"sync"

	"github.com/armon/go-proxyproto"
	"github.com/cryptogarageinc/quickfix-go/config"
)

//Acceptor accepts connections from FIX clients and manages the associated sessions.
type Acceptor struct {
	sessionMutex          sync.RWMutex
	addressMutex          sync.RWMutex
	app                   Application
	settings              *Settings
	logFactory            LogFactory
	storeFactory          MessageStoreFactory
	globalLog             Log
	sessions              map[SessionID]*session
	sessionGroup          sync.WaitGroup
	listener              net.Listener
	listenerShutdown      sync.WaitGroup
	dynamicSessions       bool
	dynamicQualifier      bool
	dynamicQualifierCount int
	dynamicSessionChan    chan *session
	allSessions           map[SessionID]*session
	sessionAddr           map[SessionID]net.Addr
	connectionValidator   ConnectionValidator
	sessionFactory
}

// ConnectionValidator is an interface allowing to implement a custom authentication logic.
type ConnectionValidator interface {
	// Validate the connection for validity. This can be a part of authentication process.
	// For example, you may tie up a SenderCompID to an IP range, or to a specific TLS certificate as a part of mTLS.
	Validate(netConn net.Conn, session SessionID) error
}

//Start accepting connections.
func (a *Acceptor) Start() error {
	socketAcceptHost := ""
	if a.settings.GlobalSettings().HasSetting(config.SocketAcceptHost) {
		var err error
		if socketAcceptHost, err = a.settings.GlobalSettings().Setting(config.SocketAcceptHost); err != nil {
			return err
		}
	}

	socketAcceptPort, err := a.settings.GlobalSettings().IntSetting(config.SocketAcceptPort)
	if err != nil {
		return err
	}

	var tlsConfig *tls.Config
	if tlsConfig, err = loadTLSConfig(a.settings.GlobalSettings()); err != nil {
		return err
	}

	var useTCPProxy bool
	if a.settings.GlobalSettings().HasSetting(config.UseTCPProxy) {
		if useTCPProxy, err = a.settings.GlobalSettings().BoolSetting(config.UseTCPProxy); err != nil {
			return err
		}
	}

	address := net.JoinHostPort(socketAcceptHost, strconv.Itoa(socketAcceptPort))
	if tlsConfig != nil {
		if a.listener, err = tls.Listen("tcp", address, tlsConfig); err != nil {
			return err
		}
	} else if useTCPProxy {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		a.listener = &proxyproto.Listener{Listener: listener}
	} else {
		if a.listener, err = net.Listen("tcp", address); err != nil {
			return err
		}
	}

	for sessionID := range a.sessions {
		session := a.sessions[sessionID]
		a.sessionGroup.Add(1)
		go func() {
			session.run()
			a.sessionGroup.Done()
		}()
	}
	if a.dynamicSessions {
		a.dynamicSessionChan = make(chan *session)
		a.sessionGroup.Add(1)
		go func() {
			a.dynamicSessionsLoop()
			a.sessionGroup.Done()
		}()
	}
	a.listenerShutdown.Add(1)
	go a.listenForConnections()
	return nil
}

//Stop logs out existing sessions, close their connections, and stop accepting new connections.
func (a *Acceptor) Stop() {
	defer func() {
		_ = recover() // suppress sending on closed channel error
	}()

	a.listener.Close()
	a.listenerShutdown.Wait()
	if a.dynamicSessions {
		close(a.dynamicSessionChan)
	}
	for _, session := range a.sessions {
		session.stop()
	}
	a.sessionGroup.Wait()
}

//RemoteAddr Get remote IP address for a given session.
func (a *Acceptor) RemoteAddr(sessionID SessionID) (net.Addr, bool) {
	a.addressMutex.RLock()
	addr, ok := a.sessionAddr[sessionID]
	a.addressMutex.RUnlock()
	return addr, ok
}

//NewAcceptor creates and initializes a new Acceptor.
func NewAcceptor(app Application, storeFactory MessageStoreFactory, settings *Settings, logFactory LogFactory) (a *Acceptor, err error) {
	a = &Acceptor{
		app:          app,
		storeFactory: storeFactory,
		settings:     settings,
		logFactory:   logFactory,
		sessions:     make(map[SessionID]*session),
		sessionAddr:  make(map[SessionID]net.Addr),
		allSessions:  make(map[SessionID]*session),
	}
	if a.settings.GlobalSettings().HasSetting(config.DynamicSessions) {
		if a.dynamicSessions, err = settings.globalSettings.BoolSetting(config.DynamicSessions); err != nil {
			return
		}

		if a.settings.GlobalSettings().HasSetting(config.DynamicQualifier) {
			if a.dynamicQualifier, err = settings.globalSettings.BoolSetting(config.DynamicQualifier); err != nil {
				return
			}
		}
	}

	if a.globalLog, err = logFactory.Create(); err != nil {
		return
	}

	for sessionID, sessionSettings := range settings.SessionSettings() {
		sessID := sessionID
		sessID.Qualifier = ""

		if _, dup := a.sessions[sessID]; dup {
			return a, errDuplicateSessionID
		}

		if a.sessions[sessID], err = a.createSession(sessionID, storeFactory, sessionSettings, logFactory, app); err != nil {
			return
		}
		a.allSessions[sessID] = a.sessions[sessID]
	}

	return
}

func (a *Acceptor) listenForConnections() {
	defer a.listenerShutdown.Done()

	for {
		netConn, err := a.listener.Accept()
		if err != nil {
			return
		}

		go func() {
			a.handleConnection(netConn)
		}()
	}
}

func (a *Acceptor) invalidMessage(msg *bytes.Buffer, err error) {
	a.globalLog.OnEventf("Invalid Message: %s, %v", msg.Bytes(), err.Error())
}

func (a *Acceptor) handleConnection(netConn net.Conn) {
	defer func() {
		if err := recover(); err != nil {
			a.globalLog.OnEventf("Connection Terminated with Panic: %s", debug.Stack())
		}

		if err := netConn.Close(); err != nil {
			a.globalLog.OnEvent(err.Error())
		}
	}()

	reader := bufio.NewReader(netConn)
	parser := newParser(reader)

	msgBytes, err := parser.ReadMessage()
	if err != nil {
		if err == io.EOF {
			a.globalLog.OnEvent("Connection Terminated")
		} else {
			a.globalLog.OnEvent(err.Error())
		}
		return
	}

	msg := NewMessage()
	err = ParseMessage(msg, msgBytes)
	if err != nil {
		a.invalidMessage(msgBytes, err)
		return
	}

	var beginString FIXString
	if err := msg.Header.GetField(tagBeginString, &beginString); err != nil {
		a.invalidMessage(msgBytes, err)
		return
	}

	var senderCompID FIXString
	if err := msg.Header.GetField(tagSenderCompID, &senderCompID); err != nil {
		a.invalidMessage(msgBytes, err)
		return
	}

	var senderSubID FIXString
	if msg.Header.Has(tagSenderSubID) {
		if err := msg.Header.GetField(tagSenderSubID, &senderSubID); err != nil {
			a.invalidMessage(msgBytes, err)
			return
		}
	}

	var senderLocationID FIXString
	if msg.Header.Has(tagSenderLocationID) {
		if err := msg.Header.GetField(tagSenderLocationID, &senderLocationID); err != nil {
			a.invalidMessage(msgBytes, err)
			return
		}
	}

	var targetCompID FIXString
	if err := msg.Header.GetField(tagTargetCompID, &targetCompID); err != nil {
		a.invalidMessage(msgBytes, err)
		return
	}

	var targetSubID FIXString
	if msg.Header.Has(tagTargetSubID) {
		if err := msg.Header.GetField(tagTargetSubID, &targetSubID); err != nil {
			a.invalidMessage(msgBytes, err)
			return
		}
	}

	var targetLocationID FIXString
	if msg.Header.Has(tagTargetLocationID) {
		if err := msg.Header.GetField(tagTargetLocationID, &targetLocationID); err != nil {
			a.invalidMessage(msgBytes, err)
			return
		}
	}

	sessID := SessionID{BeginString: string(beginString),
		SenderCompID: string(targetCompID), SenderSubID: string(targetSubID), SenderLocationID: string(targetLocationID),
		TargetCompID: string(senderCompID), TargetSubID: string(senderSubID), TargetLocationID: string(senderLocationID),
	}

	// We have a session ID and a network connection. This seems to be a good place for any custom authentication logic.
	if a.connectionValidator != nil {
		if err := a.connectionValidator.Validate(netConn, sessID); err != nil {
			a.globalLog.OnEventf("Unable to validate a connection %v", err.Error())
			return
		}
	}

	if a.dynamicQualifier {
		a.dynamicQualifierCount++
		sessID.Qualifier = strconv.Itoa(a.dynamicQualifierCount)
	}

	var connectMutex *sync.Mutex
	a.sessionMutex.RLock()
	session, ok := a.allSessions[sessID]
	a.sessionMutex.RUnlock()
	if !ok {
		if !a.dynamicSessions {
			a.globalLog.OnEventf("Session %v not found for incoming message: %s", sessID, msgBytes)
			return
		}
		dynamicSession, err := a.sessionFactory.createSession(sessID, a.storeFactory, a.settings.globalSettings.clone(), a.logFactory, a.app)
		if err != nil {
			a.globalLog.OnEventf("Dynamic session %v failed to create: %v", sessID, err)
			return
		}

		dynamicSession.connectMutex.Lock()

		a.dynamicSessionChan <- dynamicSession
		session = dynamicSession
		a.sessionMutex.Lock()
		a.allSessions[sessID] = session
		a.sessionMutex.Unlock()
		defer func() {
			a.sessionMutex.Lock()
			delete(a.allSessions, sessID)
			a.sessionMutex.Unlock()
			session.stop()
		}()
	} else {
		session.connectMutex.Lock()
	}
	connectMutex = &session.connectMutex
	defer func() {
		if connectMutex != nil {
			connectMutex.Unlock()
		}
	}()

	a.addressMutex.Lock()
	a.sessionAddr[sessID] = netConn.RemoteAddr()
	a.addressMutex.Unlock()
	msgIn := make(chan fixIn)
	msgOut := make(chan []byte)

	if err := session.connect(msgIn, msgOut); err != nil {
		a.globalLog.OnEventf("Unable to accept %v", err.Error())
		return
	}
	connectMutex = nil

	go func() {
		msgIn <- fixIn{msgBytes, parser.lastRead}
		readLoop(parser, msgIn)
	}()

	writeLoop(netConn, msgOut, a.globalLog)
}

func (a *Acceptor) dynamicSessionsLoop() {
	var id int
	var sessions = map[int]*session{}
	var complete = make(chan int)
	defer close(complete)
LOOP:
	for {
		select {
		case session, ok := <-a.dynamicSessionChan:
			if !ok {
				for _, oldSession := range sessions {
					oldSession.stop()
				}
				break LOOP
			}
			id++
			sessionID := id
			sessions[sessionID] = session
			go func() {
				session.run()
				err := UnregisterSession(session.sessionID)
				if err != nil {
					a.globalLog.OnEventf("Unregister dynamic session %v failed: %v", session.sessionID, err)
					return
				}
				complete <- sessionID
			}()
		case id := <-complete:
			session, ok := sessions[id]
			if ok {
				a.addressMutex.Lock()
				delete(a.sessionAddr, session.sessionID)
				a.addressMutex.Unlock()
				delete(sessions, id)
			} else {
				a.globalLog.OnEventf("Missing dynamic session %v!", id)
			}
		}
	}

	if len(sessions) == 0 {
		return
	}

	for id := range complete {
		delete(sessions, id)
		if len(sessions) == 0 {
			return
		}
	}
}

// SetConnectionValidator sets an optional connection validator.
// Use it when you need a custom authentication logic that includes lower level interactions,
// like mTLS auth or IP whitelistening.
// To remove a previously set validator call it with a nil value:
// 	a.SetConnectionValidator(nil)
func (a *Acceptor) SetConnectionValidator(validator ConnectionValidator) {
	a.connectionValidator = validator
}

// append API ------------------------------------------------------------------

// GetSessionIDs This function returns managed all sessionID list.
func (a *Acceptor) GetSessionIDs() []SessionID {
	a.sessionMutex.RLock()
	defer a.sessionMutex.RUnlock()
	sessionIds := make([]SessionID, 0, len(a.allSessions))
	for sessionID := range a.allSessions {
		sessionIds = append(sessionIds, sessionID)
	}
	return sessionIds
}

// GetAliveSessionIDs This function returns loggedOn sessionID list.
func (a *Acceptor) GetAliveSessionIDs() []SessionID {
	a.sessionMutex.RLock()
	defer a.sessionMutex.RUnlock()
	sessionIds := make([]SessionID, 0, len(a.allSessions))
	for sessionID, session := range a.allSessions {
		if session.IsLoggedOn() {
			sessionIds = append(sessionIds, sessionID)
		}
	}
	return sessionIds
}

// IsAliveSession This function checks if the session is a logged on session or not.
func (a *Acceptor) IsAliveSession(sessionID SessionID) bool {
	a.sessionMutex.RLock()
	defer a.sessionMutex.RUnlock()
	session, ok := a.allSessions[sessionID]
	if ok {
		return session.IsLoggedOn()
	}
	return false
}

// SendToAliveSession This function send message for logged on session.
func (a *Acceptor) SendToAliveSession(m Messagable, sessionID SessionID) error {
	msg := m.ToMessage()
	a.sessionMutex.RLock()
	session, ok := a.allSessions[sessionID]
	a.sessionMutex.RUnlock()
	if !ok {
		return errDoNotLoggedOnSession
	}
	if !session.IsLoggedOn() {
		return errDoNotLoggedOnSession
	}
	return session.queueForSend(msg)
}

// SendToAliveSessions This function send messages for logged on sessions.
func (a *Acceptor) SendToAliveSessions(m Messagable) (err error) {
	sessionIDs := a.GetAliveSessionIDs()

	errorByID := ErrorBySessionID{}
	baseMsg := m.ToMessage()
	for _, sessionID := range sessionIDs {
		msg := NewMessage()
		baseMsg.CopyInto(msg)
		msg = fillHeaderBySessionID(msg, sessionID)
		tmpErr := a.SendToAliveSession(msg, sessionID)
		if tmpErr != nil {
			errorByID.ErrorMap[sessionID] = tmpErr
		}
	}
	if len(errorByID.ErrorMap) > 0 {
		err = &errorByID
		errorByID.error = errors.New("failed to SendToAliveSessions")
	}
	return err
}
