package maimai

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type MsgLogEvent struct {
	Parent   string `json:"id"`
	UserID   string `json:"userID"`
	UserName string `json:"userName"`
	Time     int64  `json:"time"`
	Content  string `json:"content"`
}

func prepareMsgLogEvent(msg *Message) (string, *MsgLogEvent) {
	msgLogEvent := &MsgLogEvent{
		Parent:   msg.Parent,
		UserID:   msg.Sender.ID,
		UserName: msg.Sender.Name,
		Time:     msg.Time,
		Content:  msg.Content}
	return msg.ID, msgLogEvent
}

var linkMatcher = regexp.MustCompile("(https?://)?[\\S]+\\.[\\S][\\S]+[\\S^\\.]")

// Handler describes functions that process packets.
type Handler func(room *Room, input chan PacketEvent, cmdChan chan string)

// PingEventHandler processes a ping-event and replies with a ping-reply.
func PingEventHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != PingEventType {
				continue
			}
			payload, err := packet.Payload()
			if err != nil {
				room.errChan <- err
				return
			}
			data, ok := payload.(*PingEvent)
			if !ok {
				room.errChan <- errors.New("Could not assert payload as *PingEvent.")
				return
			}
			room.sendPing(data.Time)
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func isValidPingCommand(payload *Message) bool {
	if len(payload.Content) >= 5 && payload.Content[0:5] == "!ping" {
		return true
	}
	return false
}

// PingCommandHandler handles a send-event, checks for a !ping, and replies.
func PingCommandHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != SendEventType {
				continue
			}
			data := GetMessagePayload(&packet)
			if isValidPingCommand(data) {
				room.SendText("pong!", data.ID)
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

// SeenRecordHandler handles a send-event and records that the sender was seen.
func SeenRecordHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != SendEventType {
				continue
			}
			data := GetMessagePayload(&packet)
			user := strings.Replace(data.Sender.Name, " ", "", -1)
			t := time.Now().Unix()
			err := room.storeSeen(user, t)
			if err != nil {
				room.errChan <- err
				return
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func isValidSeenCommand(payload *Message) bool {
	if len(payload.Content) >= 5 &&
		payload.Content[0:5] == "!seen" &&
		string(payload.Content[6]) == "@" &&
		len(strings.Split(payload.Content, " ")) == 2 {
		return true
	}
	return false
}

// SeenCommandHandler handles a send-event, checks if !seen command was given, and responds.
// TODO : make seen record a time when a user joins a room or changes their nick
func SeenCommandHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != SendEventType {
				continue
			}
			data := GetMessagePayload(&packet)
			if isValidSeenCommand(data) {
				trimmed := strings.TrimSpace(data.Content)
				splits := strings.Split(trimmed, " ")
				lastSeen, err := room.retrieveSeen(splits[1][1:])
				if err != nil {
					room.errChan <- err
					return
				}
				if lastSeen == nil {
					room.SendText("User has not been seen yet.", data.ID)
					continue
				}
				lastSeenInt, _ := strconv.Atoi(string(lastSeen))
				lastSeenTime := time.Unix(int64(lastSeenInt), 0)
				since := time.Since(lastSeenTime)
				room.SendText(fmt.Sprintf("Seen %v hours ago.",
					int(since.Hours())), data.ID)
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func extractTitleFromTree(z *html.Tokenizer) string {
	depth := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.TextToken:
			if depth > 0 {
				title := strings.TrimSpace(string(z.Text()))
				if title == "Imgur" {
					return ""
				}
				return title
			}
		case html.StartTagToken:
			tn, _ := z.TagName()
			if string(tn) == "title" {
				depth++
			}
		}
	}
}

func getLinkTitle(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Bad response code: %v", resp.StatusCode)
	}
	z := html.NewTokenizer(resp.Body)
	return extractTitleFromTree(z), nil
}

// LinkTitleHandler handles a send-event, looks for URLs, and replies with the
// title text of a link if a valid one is found.
func LinkTitleHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != SendEventType {
				continue
			}
			data := GetMessagePayload(&packet)
			urls := linkMatcher.FindAllString(data.Content, -1)
			for _, url := range urls {
				if !strings.HasPrefix(url, "http") {
					url = "http://" + url
				}
				title, err := getLinkTitle(url)
				if err == nil && title != "" {
					room.SendText("Link title: "+title, data.ID)
					break
				}
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

// UptimeCommandHandler handlers a send-event and if the command is given
// replies with the time since the bot was started.
func UptimeCommandHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != SendEventType {
				continue
			}
			data := GetMessagePayload(&packet)
			if data.Content == "!uptime" {
				since := time.Since(room.uptime)
				room.SendText(fmt.Sprintf(
					"This bot has been up for %s.",
					since.String()),
					data.ID)
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func ScritchCommandHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != SendEventType {
				continue
			}
			data := GetMessagePayload(&packet)
			if data.Content == "!scritch" {
				room.SendText("/me bruxes",
					data.ID)
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func DebugHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Error != "" {
				room.Logger.Errorf("Packet received containing error: %s", packet.Error)
			}
			if packet.Type == BounceEventType {
				payload, err := packet.Payload()
				if err != nil {
					continue
				}
				data, ok := payload.(*BounceEvent)
				if !ok {
					continue
				}
				room.Logger.Errorf("BounceEvent received, reason: %s and ID: %s", data.Reason, packet.ID)
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func NickChangeHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != NickEventType {
				continue
			}
			data := GetNickEventPayload(&packet)
			// Don't want to process joins or leaves here
			if data.From == "" || data.To == "" {
				continue
			}
			room.SendText(fmt.Sprintf("< %s is now known as %s. >", data.From, data.To), "")
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func partTimer(room *Room, user string) {
	time.Sleep(time.Duration(5) * time.Minute)
	if room.isUserLeaving(user) && user != "" {
		room.SendText(fmt.Sprintf("< %s left the room. >", user), "")
		room.clearUserLeaving(user)
	}
}

func PartEventHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != PartEventType {
				continue
			}
			data := GetPresenceEventPayload(&packet)
			user := data.User.Name
			room.setUserLeaving(user)
			go partTimer(room, user)
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func JoinEventHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			if packet.Type != JoinEventType && packet.Type != NickEventType {
				continue
			}
			switch packet.Type {
			case JoinEventType:
				data := GetPresenceEventPayload(&packet)
				user := data.User.Name
				if user == "" {
					continue
				}
				if !room.isUserLeaving(user) {
					room.SendText(fmt.Sprintf("< %s joined the room. >", user), "")
				}
				room.clearUserLeaving(user)
			case NickEventType:
				data := GetNickEventPayload(&packet)
				if data.From != "" {
					continue
				}
				if !room.isUserLeaving(data.To) {
					room.SendText(fmt.Sprintf("< %s joined the room. >", data.To), "")
				}
				room.clearUserLeaving(data.To)
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}

func MessageLogHandler(room *Room, input chan PacketEvent, cmdChan chan string) {
	for {
		select {
		case packet := <-input:
			switch packet.Type {
			case SendEventType:
				data := GetMessagePayload(&packet)
				msgID, msgLogEvent := prepareMsgLogEvent(data)
				room.storeMsgLogEvent(msgID, msgLogEvent)
			case SendReplyType:
				data := GetMessagePayload(&packet)
				msgID, msgLogEvent := prepareMsgLogEvent(data)
				room.storeMsgLogEvent(msgID, msgLogEvent)
			}
		case cmd := <-cmdChan:
			if cmd == "kill" {
				return
			}
		}
	}
}
