package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

func setMemberStatusMode(nick, mode string, ircCh *IRCChan) (int, error) {
	if ircCh == nil {
		return -1, fmt.Errorf("setMemberStatusMode - ircCh is nil")
	}
	ircCh.Mtx.Lock()
	defer ircCh.Mtx.Unlock()
	modeChange := mode[0]
	modeType := mode[1]
	v := false

	switch modeChange {
	case '+':
		v = true
	case '-':
		v = false
	default:
		return 2, fmt.Errorf("setMemberStatusMode - unknown modechange - %s", mode)
	}

	switch modeType {
	case 'v':
		ircCh.CanTalk[nick] = v
	case 'o':
		ircCh.OpNicks[nick] = v
	default:
		return 2, fmt.Errorf("setMemberStatusMode - unknown modetype - %s", mode)
	}

	return 0, nil
}

func setChannelMode(ircCh *IRCChan, mode string) error {
	if len(mode) != 2 {
		return fmt.Errorf("setChannelMode - invalid mode")
	}
	modeChange := mode[0]
	modeType := mode[1]
	modeValue := false

	switch modeChange {
	case '+':
		modeValue = true
	case '-':
		modeValue = false
	default:
		return fmt.Errorf("setChannelMode - invalid mode")
	}

	ircCh.Mtx.Lock()

	switch modeType {
	case 'm':
		ircCh.isModerated = modeValue
	case 't':
		ircCh.isTopicRestricted = modeValue
	default:
		return fmt.Errorf("setChannelMode - invalid mode")
	}
	defer ircCh.Mtx.Unlock()

	return nil
}

func handleMode(ic *IRCConn, im IRCMessage) error {
	// target can be nick or channel
	target := im.Params[0]
	if strings.HasPrefix(target, "#") {
		fmt.Println("target has prefix #")
		// dealing with channel
		chanName := target
		ircCh, ok := lookupChannelByName(chanName)
		if !ok {
			// channel doesn't exist
			msg, _ := formatReply(ic, replyMap["ERR_NOSUCHCHANNEL"], []string{chanName})
			return sendMessage(ic, msg)
		}
		switch len(im.Params) {
		case 1:
			// Requesting channel mode
			channelMode, _ := getChannelMode(ircCh)
			msg := fmt.Sprintf(":%s!%s@%s 324 %s %s +%s\r\n", ic.Nick, ic.User, ic.Conn.LocalAddr(), ic.Nick, chanName, channelMode)
			return sendMessage(ic, msg)
		case 2:
			// Modifying channel mode
			return handleChannelModeChange(im, ic, ircCh)
		case 3:
			fmt.Println("params has length 3")
			// Modifying channelMemberStatus
			mode := im.Params[1]
			nick := im.Params[2]
			// TODO check that sender can set modes
			if !(userIsChannelOp(ic, ircCh) || userIsOp(ic)) {
				msg, _ := formatReply(ic, replyMap["ERR_CHANOPRIVSNEEDED"], []string{chanName})
				return sendMessage(ic, msg)
			}
			// check if user is in channel
			//ircCh.Mtx.Lock()
			////nickIsChannelMember := false
			////for _, v := range ircCh.Members {
			////	if v.Nick == nick {
			////		nickIsChannelMember = true
			////		break
			////	}
			////}
			//ircCh.Mtx.Unlock()
			if !ircCh.nickIsMember(nick) {
				msg, _ := formatReply(ic, replyMap["ERR_USERNOTINCHANNEL"], []string{nick, chanName})
				return sendMessage(ic, msg)
			}

			fmt.Println("setting member status mode")
			// set Mode
			rv, err := setMemberStatusMode(nick, mode, ircCh)
			if err != nil {
				switch rv {
				case 2:
					modeChar := string(mode[1])
					msg, _ := formatReply(ic, replyMap["ERR_UNKNOWNMODE"], []string{modeChar, chanName})
					return sendMessage(ic, msg)
				}
			}

			fmt.Println("sending message to channel")
			rpl := fmt.Sprintf(":%s!%s@%s %s %s %s %s\r\n", ic.Nick, ic.User, ic.Conn.LocalAddr(), im.Command, im.Params[0], im.Params[1], im.Params[2])
			sendMessageToChannel(ic, rpl, ircCh, true)

		default: // invalid num params
		}
	} else {
		// dealing with user nick
		nick := target
		if nick != ic.Nick {
			msg, _ := formatReply(ic, replyMap["ERR_USERSDONTMATCH"], []string{})
			return sendMessage(ic, msg)
			// Send some error
		}
		mode := im.Params[1]
		if len(mode) != 2 {
			rpl, _ := replyMap["ERR_UMODEUNKNOWNFLAG"]
			msg, _ := formatReply(ic, rpl, []string{})
			return sendMessage(ic, msg)
		}
		modeChange := mode[0]
		modeType := mode[1]
		modeValue := false
		switch modeChange {
		case '+':
			modeValue = true
		case '-':
			modeValue = false
		default:
			rpl, _ := replyMap["ERR_UMODEUNKNOWNFLAG"]
			msg, _ := formatReply(ic, rpl, []string{})
			return sendMessage(ic, msg)
		}
		switch modeType {
		case 'o':
			//return nil
			//fmt.Println("Handling operator case")
			//ic.isOperator = modeValue
			if modeValue {
				return nil
			} else {
				rpl := fmt.Sprintf(":%s %s %s :%s\r\n", ic.Nick, im.Command, im.Params[0], im.Params[1])
				return sendMessage(ic, rpl)
			}

		case 'a':
			return nil
		default:
			rpl, _ := replyMap["ERR_UMODEUNKNOWNFLAG"]
			msg, _ := formatReply(ic, rpl, []string{})
			return sendMessage(ic, msg)
		}
	}
	return nil
}

func handleLUsers(ic *IRCConn, im IRCMessage) error {
	writeLUsers(ic)
	return nil
}

func writeLUsers(ic *IRCConn) error {
	numServers, numServices, numOperators, numChannels := 1, 0, 0, 0
	numUsers, numUnknownConnections, numClients := 0, 0, 0

	connsMtx.Lock()
	for _, conn := range ircConns {
		if conn.Welcomed {
			numUsers++
			numClients++
		} else if conn.Nick != "*" || conn.User != "" {
			numClients++
		} else {
			numUnknownConnections++
		}
	}
	connsMtx.Unlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(":%s 251 %s :There are %d users and %d services on %d servers\r\n",
		ic.Conn.LocalAddr(), ic.Nick, numUsers, numServices, numServers))

	sb.WriteString(fmt.Sprintf(":%s 252 %s %d :operator(s) online\r\n",
		ic.Conn.LocalAddr(), ic.Nick, numOperators))

	sb.WriteString(fmt.Sprintf(":%s 253 %s %d :unknown connection(s)\r\n",
		ic.Conn.LocalAddr(), ic.Nick, numUnknownConnections))

	sb.WriteString(fmt.Sprintf(":%s 254 %s %d :channels formed\r\n",
		ic.Conn.LocalAddr(), ic.Nick, numChannels))

	sb.WriteString(fmt.Sprintf(":%s 255 %s :I have %d clients and %d servers\r\n",
		ic.Conn.LocalAddr(), ic.Nick, numClients, numServers))

	err := sendMessage(ic, sb.String())
	if err != nil {
		return fmt.Errorf("writeLusers - %w", err)
	}
	return nil
}

func handleWhoIs(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")
	targetNick := strings.Trim(params, " ")

	if targetNick == "" {
		return nil
	}

	targetIc, ok := lookupNickConn(targetNick)

	if !ok {
		rpl := replyMap["ERR_NOSUCHNICK"]
		msg, _ := formatReply(ic, rpl, []string{targetNick})
		err := sendMessage(ic, msg)
		if err != nil {
			return fmt.Errorf("whoIs - sending NOSUCHNICK - %w", err)
		}
		return nil
	}

	channelList := getConnectionChannelsString(targetIc)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(":%s 311 %s %s %s %s * :%s\r\n",
		ic.Conn.LocalAddr(), ic.Nick, targetIc.Nick, targetIc.User, targetIc.Conn.RemoteAddr().String(), targetIc.RealName))

	if channelList != "" {
		sb.WriteString(fmt.Sprintf(":%s 319 %s %s :%s\r\n",
			ic.Conn.LocalAddr(), ic.Nick, targetIc.Nick, channelList))
	}

	// RPL_WHOISSERVER
	sb.WriteString(fmt.Sprintf(":%s 312 %s %s %s :%s\r\n",
		ic.Conn.LocalAddr(), ic.Nick, targetIc.Nick, targetIc.Conn.LocalAddr().String(), "<server info>"))

	// RPL_AWAY
	//sb.WriteString(fmt.Sprintf(":%s 301 %s %s %s :%s\r\n",
	//	ic.Conn.LocalAddr(), ic.Nick, targetIc.Nick, targetIc.Conn.LocalAddr().String(), "<server info>"))

	// RPL_WHOISOPERATOR
	//sb.WriteString(fmt.Sprintf(":%s 313 %s %s %s :%s\r\n",
	//	ic.Conn.LocalAddr(), ic.Nick, targetIc.Nick, targetIc.Conn.LocalAddr().String(), "<server info>"))

	sb.WriteString(fmt.Sprintf(":%s 318 %s %s :End of WHOIS list\r\n",
		ic.Conn.LocalAddr(), ic.Nick, targetIc.Nick))

	err := sendMessage(ic, sb.String())
	if err != nil {
		return fmt.Errorf("whoIs - %w", err)
	}

	return nil
}

func handleDefault(ic *IRCConn, im IRCMessage) error {
	command := im.Command
	if command == "" || !ic.Welcomed {
		return nil
	}

	rplName := "ERR_UNKNOWNCOMMAND"
	msg, _ := formatReply(ic, replyMap[rplName], []string{command})
	err := sendMessage(ic, msg)
	if err != nil {
		return fmt.Errorf("handleDefault - sending %s %w", rplName, err)
	}
	return nil
}

// Helper function to deal with the fact that nickToConn should be threadsafe
func lookupNickConn(nick string) (*IRCConn, bool) {
	nickToConnMtx.Lock()
	recipientIc, ok := nickToConn[nick]
	nickToConnMtx.Unlock()
	return recipientIc, ok
}

func handleNotice(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")
	// TODO handle channels
	splitParams := strings.SplitN(params, " ", 2)
	if len(splitParams) < 2 {
		return nil
	}
	targetNick, userMessage := splitParams[0], splitParams[1]

	// get connection from targetNick
	recipientIc, ok := lookupNickConn(targetNick)

	if !ok {
		// TODO this should probably log something
		return nil
	}

	msg := fmt.Sprintf(
		":%s!%s@%s NOTICE %s :%s\r\n",
		ic.Nick, ic.User, ic.Conn.RemoteAddr(), targetNick, userMessage)
	err := sendMessage(recipientIc, msg)
	if err != nil {
		return fmt.Errorf("handleNotice - %w", err)
	}
	return nil
}

func handleMotd(ic *IRCConn, im IRCMessage) error {
	return writeMotd(ic)
}

func writeMotd(ic *IRCConn) error {
	dat, err := os.ReadFile("./motd.txt")
	if err != nil {
		rplName := "ERR_NOMOTD"
		rpl := replyMap[rplName]
		msg, _ := formatReply(ic, rpl, []string{})
		err := sendMessage(ic, msg)
		if err != nil {
			return fmt.Errorf("writeMotd - sending %s %w", rplName, err)
		}
		return nil
	}
	motd := string(dat)

	motdLines := strings.FieldsFunc(motd, func(c rune) bool { return c == '\n' || c == '\r' })

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(":%s 375 %s :- %s Message of the day - \r\n",
		ic.Conn.LocalAddr(), ic.Nick, ic.Conn.LocalAddr()))

	for _, motdLine := range motdLines {
		log.Println(motdLine)
		sb.WriteString(fmt.Sprintf(":%s 372 %s :- %s\r\n",
			ic.Conn.LocalAddr(), ic.Nick, string(motdLine)))
	}

	sb.WriteString(fmt.Sprintf(":%s 376 %s :End of MOTD command\r\n",
		ic.Conn.LocalAddr(), ic.Nick))

	err = sendMessage(ic, sb.String())
	if err != nil {
		return fmt.Errorf("writeMotd - sending MOTD %w", err)
	}
	return nil
}

func handlePing(ic *IRCConn, im IRCMessage) error {
	// TODO validate welcome?
	// TODO update ping to update connection lifetime?
	msg := fmt.Sprintf("PONG %s\r\n", ic.Conn.LocalAddr().String())
	err := sendMessage(ic, msg)
	if err != nil {
		return fmt.Errorf("handlePing - %w", err)
	}
	return nil
}

func handlePong(ic *IRCConn, im IRCMessage) error {
	return nil
}

func handleChannelPrivMsg(ic *IRCConn, im IRCMessage, ircCh *IRCChan) error {
	target := im.Params[0]

	if ircCh.nickCanSendPM(ic.Nick) {
		msg := fmt.Sprintf(
			":%s!%s@%s PRIVMSG %s :%s\r\n",
			ic.Nick, ic.User, ic.Conn.RemoteAddr(), target, im.Params[1])
		if len(msg) > 512 {
			msg = msg[:510] + "\r\n"
		}

		//fmt.Printf("sending message to channel: %s", msg)
		return sendMessageToChannel(ic, msg, ircCh, false)

	} else {
		rplName := "ERR_CANNOTSENDTOCHAN"
		msg, _ := formatReply(ic, replyMap[rplName], []string{target})
		err := sendMessage(ic, msg)
		if err != nil {
			return fmt.Errorf("handlePrivMsg - writing %s - %w", rplName, err)
		}
		return nil
	}

}

func handlePrivMsg(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")

	splitParams := strings.SplitN(params, " ", 2)
	target, userMessage := strings.Trim(splitParams[0], " "), splitParams[1]

	if strings.HasPrefix(target, "#") {
		// USER TO CHANNEL PM

		// get connection from targetNick
		channel, ok := lookupChannelByName(target)

		if !ok {
			rplName := "ERR_NOSUCHNICK"
			rpl := replyMap[rplName]
			msg, _ := formatReply(ic, rpl, []string{target})
			err := sendMessage(ic, msg)
			if err != nil {
				return fmt.Errorf("handlePrivMsg - writing %s - %w", rplName, err)
			}
			return nil
		}

		return handleChannelPrivMsg(ic, im, channel)

	} else {
		// USER TO USER PM

		// get connection from targetNick
		recipientIc, ok := lookupNickConn(target)

		if !ok {
			rplName := "ERR_NOSUCHNICK"
			rpl := replyMap[rplName]
			msg, _ := formatReply(ic, rpl, []string{target})
			err := sendMessage(ic, msg)
			if err != nil {
				return fmt.Errorf("handlePrivMsg - writing %s - %w", rplName, err)
			}
			return nil
		}

		msg := fmt.Sprintf(
			":%s!%s@%s PRIVMSG %s :%s\r\n",
			ic.Nick, ic.User, ic.Conn.RemoteAddr(), target, userMessage)
		if len(msg) > 512 {
			msg = msg[:510] + "\r\n"
		}
		_, err := recipientIc.Conn.Write([]byte(msg))
		if err != nil {
			log.Fatal(err)
		}

		if recipientIc.AwayMessage != "" {
			awayAutoReply := fmt.Sprintf(":%s!%s@%s 301 %s %s :%s\r\n",
				recipientIc.Nick, recipientIc.User, recipientIc.Conn.RemoteAddr(), ic.Nick, recipientIc.Nick, recipientIc.AwayMessage)
			err := sendMessage(ic, awayAutoReply)
			if err != nil {
				return fmt.Errorf("handlePrivMsg - writing awayAutoReply - %w", err)
			}
		}
	}
	return nil
}

func handleQuit(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")

	quitMessage := "Client Quit"
	if params != "" {
		quitMessage = removePrefix(params)
	}

	msg := fmt.Sprintf("ERROR :Closing Link: %s (%s)\r\n", ic.Conn.RemoteAddr(), quitMessage)
	err := sendMessage(ic, msg)
	if err != nil {
		return fmt.Errorf("handleQuit - sending closing link - %w", err)
	}

	cleanupIC(ic)

	ic.isDeleted = true
	err = ic.Conn.Close()
	if err != nil {
		return fmt.Errorf("handleQuit - Closing connection - %w", err)
	}
	return nil
}

// CONTINUE FROM HERE
func handleNick(ic *IRCConn, im IRCMessage) error {
	//params := strings.Join(im.Params, " ")

	prevNick := ic.Nick
	//nick := strings.SplitN(params, " ", 2)[0]
	nick := im.Params[0]

	_, nickInUse := lookupNickConn(nick)
	if nick != ic.Nick && nickInUse { // TODO what happens if they try to change their own nick?
		msg, _ := formatReply(ic, replyMap["ERR_NICKNAMEINUSE"], []string{nick})
		_, err := ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Fatal(err)
		}
		return nil
	}

	// if Nick has already been set
	nickToConnMtx.Lock()
	if prevNick != "*" {
		delete(nickToConn, prevNick)
	}

	nickToConn[nick] = ic
	nickToConnMtx.Unlock()
	ic.Nick = nick

	checkAndSendWelcome(ic)
	return nil
}

func handleUser(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")

	if ic.Welcomed && ic.User != "" {
		msg := fmt.Sprintf(
			":%s 463 :You may not reregister\r\n",
			ic.Conn.LocalAddr())
		_, err := ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Fatal(err)
		}

		return nil
	}

	splitParams := strings.SplitN(params, " ", 2)
	ic.User = splitParams[0]
	splitOnColon := strings.SplitN(splitParams[1], ":", 2)
	if len(splitOnColon) > 1 {
		ic.RealName = splitOnColon[1]
	} else {
		ic.RealName = strings.SplitN(strings.Trim(splitParams[1], " "), " ", 3)[2]
	}

	checkAndSendWelcome(ic)
	return nil
}

func handleTopic(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")

	splitParams := strings.SplitN(params, " ", 2)

	chanName := splitParams[0]
	newTopic := ""
	if len(splitParams) >= 2 {
		newTopic = removePrefix(splitParams[1])
	}

	ircCh, ok := lookupChannelByName(chanName)
	if !ok {
		// ERR Channel doesn't exist

		msg, _ := formatReply(ic, replyMap["ERR_NOTONCHANNEL"], []string{chanName}) // This is required by tests
		_, err := ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Println("error sending ERR_NOTONCHANNEL reply")
		}
		return nil
	}

	if !ircCh.nickIsMember(ic.Nick) {
		msg, _ := formatReply(ic, replyMap["ERR_NOTONCHANNEL"], []string{chanName})
		return sendMessage(ic, msg)
	}

	if !userCanSetTopic(ic, ircCh) {
		msg, _ := formatReply(ic, replyMap["ERR_CHANOPRIVSNEEDED"], []string{chanName})
		return sendMessage(ic, msg)
	}

	ircCh.Mtx.Lock()

	var msg string
	if newTopic != "" {
		// update channel topic
		ircCh.Topic = newTopic
		msg = fmt.Sprintf(":%s!%s@%s TOPIC %s :%s\r\n",
			ic.Nick, ic.User, ic.Conn.RemoteAddr(), chanName, ircCh.Topic)
	} else {
		// read channel topic
		if ircCh.Topic == "" {
			// RPL_NOTOPIC
			msg = fmt.Sprintf(":%s 331 %s %s :No topic is set\r\n", ic.Conn.LocalAddr(), ic.Nick, chanName)
		} else {
			// RPL_TOPIC
			msg = fmt.Sprintf(":%s 332 %s %s :%s\r\n", ic.Conn.LocalAddr(), ic.Nick, chanName, ircCh.Topic)
		}
	}
	ircCh.Mtx.Unlock()

	if newTopic != "" {
		sendMessageToChannel(ic, msg, ircCh, true)
	} else {
		_, err := ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Println("error sending TOPIC reply")
		}
	}
	return nil
}

func handleAway(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")
	awayMessage := removePrefix(strings.Trim(params, " "))
	var msg string
	if awayMessage == "" {
		// clear away message
		ic.AwayMessage = ""
		ic.isAway = false
		msg = fmt.Sprintf(":%s 305 %s :You are no longer marked as being away\r\n", ic.Conn.LocalAddr(), ic.Nick)
	} else {
		ic.isAway = true
		ic.AwayMessage = awayMessage
		msg = fmt.Sprintf(":%s 306 %s :You have been marked as being away\r\n", ic.Conn.LocalAddr(), ic.Nick)
	}
	_, err := ic.Conn.Write([]byte(msg))
	if err != nil {
		log.Println("error sending AWAY reply")
	}
	return nil
}

func lookupChannelByName(name string) (*IRCChan, bool) {
	nameToChanMtx.Lock()
	ircCh, ok := nameToChan[name]
	nameToChanMtx.Unlock()
	return ircCh, ok
}

func sendMessageToChannel(senderIC *IRCConn, msg string, ircCh *IRCChan, sendToSelf bool) error {
	members := getChannelMembers(ircCh)
	for _, v := range members {
		v := v
		if sendToSelf || v != senderIC {
			go func() {
				_, err := v.Conn.Write([]byte(msg))

				if err != nil {
					log.Fatalf("sendMessageToChannel - %q", err)
				}
			}()
		}
	}
	return nil
}

func addUserToChannel(ic *IRCConn, ircCh *IRCChan) {
	ircCh.Mtx.Lock()
	ircCh.Members[ic.Nick] = ic
	ircCh.Mtx.Unlock()
	joinMsg := fmt.Sprintf(":%s!%s@%s JOIN %s\r\n", ic.Nick, ic.User, ic.Conn.RemoteAddr(), ircCh.Name)
	sendMessageToChannel(ic, joinMsg, ircCh, false)
	_, err := ic.Conn.Write([]byte(joinMsg))
	if err != nil {
		log.Fatal(err)
	}
}

// TODO need to clean up cantalk and opnick status when user leaves channel
func newChannel(ic *IRCConn, chanName string) *IRCChan {
	newChan := IRCChan{
		Mtx:     sync.Mutex{},
		Name:    chanName,
		Topic:   "",
		OpNicks: make(map[string]bool),
		CanTalk: make(map[string]bool),
		Members: make(map[string]*IRCConn),
	}
	newChan.OpNicks[ic.Nick] = true
	newChan.CanTalk[ic.Nick] = true
	chansMtx.Lock()
	ircChans = append(ircChans, &newChan)
	chansMtx.Unlock()

	nameToChanMtx.Lock()
	nameToChan[chanName] = &newChan
	nameToChanMtx.Unlock()
	return &newChan
}

func sendTopicReply(ic *IRCConn, ircCh *IRCChan) {
	// Send channel topic to ic
	// if channel topic is not sent, send RPL_NOTOPIC instead
	topic := "No topic is set"
	rplCode := 331
	if ircCh.Topic != "" {
		topic = ircCh.Topic
		rplCode = 332
	} else {
		return
	}
	topicReply := fmt.Sprintf(":%s %03d %s %s :%s\r\n", ic.Conn.LocalAddr(), rplCode, ic.Nick, ircCh.Name, topic)
	_, err := ic.Conn.Write([]byte(topicReply))
	if err != nil {
		log.Fatal(err)
	}
}

func getConnectionChannelsString(ic *IRCConn) string {
	memberChannels := ""
	connChans := getConnectionChannels(ic)
	chansMtx.Lock()
	for _, c := range connChans {
		// isMember locks the corresponding channel's mutex
		// this should be okay b/c the ordering of mutex acquisition is
		// always chansMtx, then channel specific mtx

		op := userIsChannelOp(ic, c)
		v := c.chanIsModerated() && userHasVoice(ic, c)
		if op {
			memberChannels += "@"
		}
		if v {
			memberChannels += "+"
		}
		memberChannels += c.Name + " "
	}
	defer chansMtx.Unlock()
	return memberChannels
}

func getConnectionChannels(ic *IRCConn) []*IRCChan {
	connChans := []*IRCChan{}
	chansMtx.Lock()
	for _, c := range ircChans {
		if c.isMember(ic) {
			connChans = append(connChans, c)
		}
	}
	chansMtx.Unlock()
	return connChans
}

func getChannelMembers(ircCh *IRCChan) []*IRCConn {
	ircCh.Mtx.Lock()
	defer ircCh.Mtx.Unlock()
	conns := []*IRCConn{}
	for _, c := range ircCh.Members {
		conns = append(conns, c)
	}
	return conns
}

func sendNoChannelNamReply(ic *IRCConn, names []string) {
	var sb strings.Builder
	channelStatusIndicator := "*" // Using public indicator as default
	sb.WriteString(fmt.Sprintf(":%s %03d %s %s %s :", ic.Conn.LocalAddr(), 353, ic.Nick,
		channelStatusIndicator, "*"))
	members := names
	for i, v := range members {
		if i != 0 {
			sb.WriteString(" ")
		}
		// append channel member nick
		sb.WriteString(v)
	}
	sb.WriteString("\r\n")
	// Send RPL_NAMREPLY
	err := sendMessage(ic, sb.String())
	if err != nil {
		log.Fatal(err)
	}
}

func sendNamReply(ic *IRCConn, ircCh *IRCChan) {
	var sb strings.Builder
	channelStatusIndicator := "=" // Using public indicator as default
	sb.WriteString(fmt.Sprintf(":%s %03d %s %s %s :", ic.Conn.LocalAddr(), 353, ic.Nick,
		channelStatusIndicator, ircCh.Name))

	//fmt.Printf("Sending name reply for channel %s\n", ircCh.Name)
	ircCh.printChannel()

	members := getChannelMembers(ircCh)
	for i, m := range members {
		n := m.Nick
		//fmt.Printf("adding nick %s to list\n", n)
		if i != 0 {
			sb.WriteString(" ")
		}
		userConn, ok := lookupNickConn(n)
		if ok {
			//fmt.Printf("IN OK\n")
			if userIsChannelOp(userConn, ircCh) {
				//fmt.Printf("nick %s is chanOp\n", n)
				sb.WriteString("@")
			} else {
				//fmt.Printf("IN ELSE\n")
				if userHasVoice(userConn, ircCh) {
					//fmt.Printf("nick %s has voice\n", n)
					sb.WriteString("+")
				}
				fmt.Printf("OUT ELSE\n")
			}
		} else {
			fmt.Fprintf(os.Stderr, "Could not find connection for nick: %s\n", n)
		}

		//user, ok := ircCh.OpNicks[n]
		//if present && ok {
		//}
		// append channel member nick
		sb.WriteString(n)
	}
	sb.WriteString("\r\n")
	// Send RPL_NAMREPLY
	err := sendMessage(ic, sb.String())
	if err != nil {
		log.Fatal(err)
	}
}

func sendEndOfNames(ic *IRCConn, chanName string) error {
	endOfNames := fmt.Sprintf(":%s %03d %s %s :End of NAMES list\r\n", ic.Conn.LocalAddr(), 366, ic.Nick, chanName)
	_, err := ic.Conn.Write([]byte(endOfNames))
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func handlePart(ic *IRCConn, im IRCMessage) error {
	chanName := im.Params[0]
	ircCh, ok := lookupChannelByName(chanName)
	if !ok {
		// ERR Channel doesn't exist
		msg, _ := formatReply(ic, replyMap["ERR_NOSUCHCHANNEL"], []string{chanName})
		return sendMessage(ic, msg)
	}

	memberOfChannel := ircCh.nickIsMember(ic.Nick)

	if !memberOfChannel {
		msg, _ := formatReply(ic, replyMap["ERR_NOTONCHANNEL"], []string{chanName})
		_, err := ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Println("error sending ERR_NOTONCHANNEL reply")
		}
		return nil
	}

	msg := ""
	if len(im.Params) == 2 {
		msg = fmt.Sprintf(":%s!%s@%s PART %s :%s\r\n", ic.Nick, ic.User, ic.Conn.RemoteAddr(), chanName, removePrefix(im.Params[1]))
	} else {
		msg = fmt.Sprintf(":%s!%s@%s PART %s\r\n", ic.Nick, ic.User, ic.Conn.RemoteAddr(), chanName)
	}

	sendMessageToChannel(ic, msg, ircCh, true)

	numChannelMembers := 0
	// Remove user from channel
	ircCh.Mtx.Lock()
	delete(ircCh.Members, ic.Nick)

	if ircCh.OpNicks[ic.Nick] {
		delete(ircCh.OpNicks, ic.Nick)
	}

	// Delete channel if nobody in it
	numChannelMembers = len(ircCh.Members)
	ircCh.Mtx.Unlock()
	channelIndex := 0
	if numChannelMembers == 0 {
		nameToChanMtx.Lock()
		chansMtx.Lock()
		delete(nameToChan, chanName)

		for i, v := range ircChans {
			if v == ircCh {
				channelIndex = i
				break
			}
		}
		ircChans[channelIndex] = ircChans[len(ircChans)-1]
		ircChans = ircChans[:len(ircChans)-1]
		chansMtx.Unlock()
		nameToChanMtx.Unlock()
	}
	return nil
}

func handleJoin(ic *IRCConn, im IRCMessage) error {
	params := strings.Join(im.Params, " ")
	chanName := params

	ircCh, ok := lookupChannelByName(chanName)
	if !ok {
		// Create new channel
		ircCh = newChannel(ic, chanName)
	}

	if ircCh.nickIsMember(ic.Nick) {
		return nil
	}
	// Join channel
	addUserToChannel(ic, ircCh)
	// RPL_TOPIC
	sendTopicReply(ic, ircCh)
	// RPL_NAMREPLY
	sendNamReply(ic, ircCh)
	// RPL_ENDOFNAMES
	sendEndOfNames(ic, ircCh.Name)
	return nil
}

func handleList(ic *IRCConn, im IRCMessage) error {
	var sb strings.Builder
	chansMtx.Lock()
	for _, ircChan := range ircChans {
		numMembers := len(getChannelMembers(ircChan))
		sb.WriteString(fmt.Sprintf(
			":%s 322 %s %s %d :%s\r\n",
			ic.Conn.LocalAddr(), ic.Nick, ircChan.Name, numMembers, ircChan.Topic))
	}
	sb.WriteString(fmt.Sprintf(
		":%s 323 %s :End of LIST\r\n",
		ic.Conn.LocalAddr(), ic.Nick))
	chansMtx.Unlock()
	_, err := ic.Conn.Write([]byte(sb.String()))
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func checkAndSendWelcome(ic *IRCConn) {
	if !ic.Welcomed && ic.Nick != "*" && ic.User != "" {
		msg := fmt.Sprintf(
			":%s 001 %s :Welcome to the Internet Relay Network %s!%s@%s\r\n",
			ic.Conn.LocalAddr(), ic.Nick, ic.Nick, ic.User, ic.Conn.RemoteAddr().String())

		log.Printf(msg)
		_, err := ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Fatal(err)
		}
		ic.Welcomed = true

		// RPL_YOURHOST
		msg = fmt.Sprintf(
			":%s 002 %s :Your host is %s, running version %s\r\n",
			ic.Conn.LocalAddr(), ic.Nick, ic.Conn.LocalAddr(), VERSION)

		log.Printf(msg)
		_, err = ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Fatal(err)
		}

		// RPL_CREATED
		msg = fmt.Sprintf(
			":%s 003 %s :This server was created %s\r\n",
			ic.Conn.LocalAddr(), ic.Nick, timeCreated)

		log.Printf(msg)
		_, err = ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Fatal(err)
		}

		// RPL_MYINFO
		msg = fmt.Sprintf(
			":%s 004 %s %s %s %s %s\r\n",
			ic.Conn.LocalAddr(), ic.Nick, ic.Conn.LocalAddr(), VERSION, "ao", "mtov")

		log.Printf(msg)
		_, err = ic.Conn.Write([]byte(msg))
		if err != nil {
			log.Fatal(err)
		}

		writeLUsers(ic)
		writeMotd(ic)
	}
}

// Caution: Side effects! - This modifies m
func excludeFromMap(m map[*IRCConn]bool, t []*IRCConn) map[*IRCConn]bool {
	for _, c := range t {
		_, ok := m[c]
		if ok {
			delete(m, c)
		}
	}
	return m
}

func handleNames(ic *IRCConn, im IRCMessage) error {
	l := len(im.Params)
	if l == 0 {
		connsWithChannels := []*IRCConn{}
		chansMtx.Lock()
		for _, c := range ircChans {
			sendNamReply(ic, c)
			chanMembers := getChannelMembers(c)
			for _, m := range chanMembers {
				connsWithChannels = append(connsWithChannels, m)
			}
		}
		chansMtx.Unlock()

		connsMtx.Lock()
		var conns = make([]*IRCConn, len(ircConns))
		copy(conns, ircConns)
		connsMtx.Unlock()

		connsMap := make(map[*IRCConn]bool)
		for _, c := range conns {
			log.Printf("adding %s to conns map\n", c.Nick)
			connsMap[c] = true
		}

		channellessConns := excludeFromMap(connsMap, connsWithChannels)

		if len(channellessConns) > 0 {
			log.Printf("%d connections without channels", len(channellessConns))

			var channellessConnNicks = []string{}
			for c, _ := range channellessConns {
				channellessConnNicks = append(channellessConnNicks, c.Nick)
			}
			sendNoChannelNamReply(ic, channellessConnNicks)
		}
		sendEndOfNames(ic, "*")
	} else if l == 1 {
		// list names on channel
		chanName := im.Params[0]
		ircCh, ok := lookupChannelByName(chanName)
		if ok {
			sendNamReply(ic, ircCh)
		}
		sendEndOfNames(ic, chanName)

	} else {
		// unsupported
	}
	return nil
}

func handleOper(ic *IRCConn, im IRCMessage) error {
	if len(im.Params) != 2 {
		return fmt.Errorf("handleOper - Expected 2 params, got %d", len(im.Params))
	}
	pw := im.Params[1]
	msg := ""
	if pw == *operatorPassword {
		err := makeOp(ic)
		if err != nil {
			return fmt.Errorf("handleOper - %w", err)
		}
		msg, _ = formatReply(ic, replyMap["RPL_YOUREOPER"], []string{})
	} else {
		msg, _ = formatReply(ic, replyMap["ERR_PASSWDMISMATCH"], []string{})
	}

	sendMessage(ic, msg)
	return nil
}

func handleChannelWho(ic *IRCConn, ircCh *IRCChan, chanName string) error {
	// Send who reply for each person on channel
	members := getChannelMembers(ircCh)
	hopCount := "0"

	for _, m := range members {
		mFlags := ""
		if m.isAway {
			mFlags += "G"
		} else {
			mFlags += "H"
		}
		if userIsOp(m) {
			mFlags += "*"
		}
		if userIsChannelOp(m, ircCh) {
			mFlags += "@"
		} else if userHasVoice(m, ircCh) {
			mFlags += "+"
		}
		msg, _ := formatReply(ic, replyMap["RPL_WHOREPLY"], []string{
			chanName, m.User, m.Conn.RemoteAddr().Network(),
			m.Conn.LocalAddr().Network(), m.Nick, mFlags,
			hopCount, m.RealName})
		fmt.Printf("formatted reply: %s\n", msg)

		err := sendMessage(ic, msg)
		if err != nil {
			return fmt.Errorf("handleWho - %w", err)
		}
	}
	msg, _ := formatReply(ic, replyMap["RPL_ENDOFWHO"], []string{chanName})
	err := sendMessage(ic, msg)
	if err != nil {
		return fmt.Errorf("handleWho - %w", err)
	}
	return nil
}

func handleWhoElse(ic *IRCConn) error {
	// No mask specified
	connsMtx.Lock()
	membersWithoutCommonChannels := make(map[*IRCConn]bool)
	for _, c := range ircConns {
		membersWithoutCommonChannels[c] = true
	}

	connsMtx.Unlock()

	connChans := getConnectionChannels(ic)
	for _, ch := range connChans {
		chanMembers := getChannelMembers(ch)
		for _, m := range chanMembers {
			delete(membersWithoutCommonChannels, m)
		}
	}
	//fmt.Printf("connChans len: %d\n", len(connChans))
	//chansMtx.Lock()
	//allChans := make([]*IRCChan, len(ircChans))
	//copied := copy(allChans, ircChans)
	//if copied != len(ircChans) {
	//	return fmt.Errorf("handleWhoElse - unable to copy")
	//}
	//chansWithoutConn := make(map[*IRCChan]bool)
	//// add all chans to map
	//for _, c := range allChans {
	//	chansWithoutConn[c] = true
	//}
	//fmt.Printf("chansWithoutConns before len: %d\n", len(chansWithoutConn))

	//// remove all chans that the sender is on from the map
	//for _, c := range connChans {
	//	delete(chansWithoutConn, c)
	//}

	//fmt.Printf("chansWithoutConns after len: %d\n", len(chansWithoutConn))

	//membersWithoutCommonChannels := make(map[*IRCConn]bool)
	//for ch, _ := range chansWithoutConn {
	//	chanMembers := getChannelMembers(ch)
	//	for _, m := range chanMembers {
	//		membersWithoutCommonChannels[m] = true
	//	}
	//}
	//chansMtx.Unlock()
	//fmt.Printf("membersWithoutCommonChannels len: %d\n", len(membersWithoutCommonChannels))

	for m, _ := range membersWithoutCommonChannels {
		hopCount := "0"
		mFlags := ""
		if m.isAway {
			mFlags += "G"
		} else {
			mFlags += "H"
		}
		if userIsOp(m) {
			mFlags += "*"
		}
		msg, _ := formatReply(ic, replyMap["RPL_WHOREPLY"], []string{
			"*", m.User, m.Conn.RemoteAddr().Network(), m.Conn.LocalAddr().Network(),
			m.Nick, mFlags, hopCount, m.RealName})

		fmt.Printf("formatted reply: %s\n", msg)
		err := sendMessage(ic, msg)
		if err != nil {
			return fmt.Errorf("handleWho - %w", err)
		}
	}

	msg, _ := formatReply(ic, replyMap["RPL_ENDOFWHO"], []string{"*"})
	err := sendMessage(ic, msg)
	if err != nil {
		return fmt.Errorf("handleWho - %w", err)
	}
	return nil
}

func handleWho(ic *IRCConn, im IRCMessage) error {
	fmt.Println("handling Who")
	pl := len(im.Params)
	if pl == 0 {
		return handleWhoElse(ic)
	} else if pl == 1 {
		// Channel mask specified
		chanName := im.Params[0]
		fmt.Printf("got channel mask: %s\n", chanName)
		ircCh, ok := lookupChannelByName(chanName)
		if ok {
			fmt.Println("found channel")
			handleChannelWho(ic, ircCh, chanName)
		} else {
			if im.Params[0] == "*" || im.Params[0] == "0" {
				return handleWhoElse(ic)
			}
			// Could not find channel with given name
			return fmt.Errorf("could not find requestd channel %s", im.Params[0])
		}
	} else {
		// 'o': unsupported
	}
	return nil
}
