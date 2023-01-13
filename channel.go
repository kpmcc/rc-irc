package main

import (
	"fmt"
	"log"
	"os"
	"sync"
)

type IRCChan struct {
	Mtx     sync.Mutex
	Name    string
	Topic   string
	OpNicks map[string]bool
	CanTalk map[string]bool
	//Members           []*IRCConn
	Members           map[string]*IRCConn
	isModerated       bool
	isTopicRestricted bool
}

func userCanSetTopic(ic *IRCConn, ircCh *IRCChan) bool {
	ircCh.Mtx.Lock()
	tr := ircCh.isTopicRestricted
	ircCh.Mtx.Unlock()
	if tr {
		likeOp := userIsChannelOp(ic, ircCh) || userIsOp(ic)
		return likeOp
	}

	return true
}

func userHasVoice(ic *IRCConn, ircCh *IRCChan) bool {
	ircCh.Mtx.Lock()
	defer ircCh.Mtx.Unlock()
	im := ircCh.isModerated
	ct, ok := ircCh.CanTalk[ic.Nick]
	return im && (ct && ok)

}

func userIsChannelOp(ic *IRCConn, ircCh *IRCChan) bool {
	ircCh.Mtx.Lock()
	defer ircCh.Mtx.Unlock()
	isOp, ok := ircCh.OpNicks[ic.Nick]
	return isOp && ok
}

func userIsOp(ic *IRCConn) bool {
	connsMtx.Lock()
	defer connsMtx.Unlock()
	isOp := ic.isOperator
	return isOp
}

func makeOp(ic *IRCConn) error {
	connsMtx.Lock()
	defer connsMtx.Unlock()
	if ic != nil {
		ic.isOperator = true
		return nil
	} else {
		return fmt.Errorf("makeOp - ic is Nil")
	}
}

func getChannelMode(ircCh *IRCChan) (string, error) {
	ircCh.Mtx.Lock()
	channelMode := ""
	if ircCh.isModerated {
		channelMode += "m"
	}
	if ircCh.isTopicRestricted {
		channelMode += "t"
	}
	defer ircCh.Mtx.Unlock()
	return channelMode, nil
}

func (c *IRCChan) nickIsMember(nick string) bool {
	c.Mtx.Lock()
	defer c.Mtx.Unlock()
	_, ok := c.Members[nick]
	return ok
}

func (c *IRCChan) isMember(ic *IRCConn) bool {
	c.Mtx.Lock()
	defer c.Mtx.Unlock()
	for _, m := range c.Members {
		if ic == m {
			return true
		}
	}
	return false
}

func (c *IRCChan) nickCanSendPM(nick string) bool {
	if c.nickIsMember(nick) {
		fmt.Fprintln(os.Stderr, "nickIsMember")
		if c.isModerated {
			fmt.Fprintln(os.Stderr, "isModerated")
			c.Mtx.Lock()
			senderCanTalk, ok := c.CanTalk[nick]
			senderIsChanOp, opOk := c.OpNicks[nick]
			c.Mtx.Unlock()
			ic, ok := lookupNickConn(nick)
			if !ok {
				log.Printf("nickCanSendPM: nick to conn lookup failed for %s", nick)
				return false
			}
			icIsOp := userIsOp(ic)

			fmt.Fprintf(os.Stderr, "cantalk: %t ok: %t op: %t\n", senderCanTalk, ok, icIsOp)
			canPM := (senderCanTalk && ok) || (senderIsChanOp && opOk) || icIsOp
			return canPM
		} else {
			return true
		}
	} else {
		return false
	}
}

type ModeType uint8

const (
	Moderated ModeType = iota
	TopicRestricted
	Invalid
)

func getModeType(m string) (ModeType, bool, bool) {
	if len(m) != 2 {
		return Invalid, false, false
	}

	operation := m[0]
	parameter := m[1]

	var modeValue bool = false
	var t ModeType = Invalid
	var ok bool = true

	switch parameter {
	case 'm':
		t = Moderated
	case 't':
		t = TopicRestricted
	default:
		t = Invalid
		ok = false
	}

	switch operation {
	case '+':
		modeValue = true
	case '-':
		modeValue = false
	default:
		modeValue = false
		t = Invalid
		ok = false
	}

	return t, modeValue, ok
}

func (c *IRCChan) setMode(m ModeType, v bool) bool {
	c.Mtx.Lock()
	defer c.Mtx.Unlock()
	rv := v
	switch m {
	case Moderated:
		c.isModerated = v
		rv = c.isModerated
	case TopicRestricted:
		c.isTopicRestricted = v
		rv = c.isTopicRestricted
	case Invalid:
		rv = false
	}
	return rv
}
